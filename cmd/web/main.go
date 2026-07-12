// cmd/web/main.go - точка входа веб-версии приложения.
//
// Сервер переиспользует всю бизнес-логику из internal/domain и
// internal/infra/codec - те же пакеты, что и консольная версия (cmd/app).
// Веб-слой - это просто ещё один "клиент" движка обработки изображений,
// как и говорилось при проектировании: он ничего не знает о деталях
// сжатия/обрезки/водяных знаков, только принимает HTTP-запросы и вызывает
// CompressorService.
package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/QtaroAXE/image-redactor/internal/domain/compression"
	apperrors "github.com/QtaroAXE/image-redactor/internal/domain/errors"
	"github.com/QtaroAXE/image-redactor/internal/domain/imginfo"
	compressor "github.com/QtaroAXE/image-redactor/internal/infra/codec"
	"github.com/QtaroAXE/image-redactor/internal/infra/fs"
)

//go:embed static/index.html
var staticFiles embed.FS

func main() {
	addr := flag.String("addr", ":8080", "адрес, на котором слушает сервер")
	uploadsDir := flag.String("uploads-dir", "./web-data/uploads", "директория для временно загруженных файлов")
	resultsDir := flag.String("results-dir", "./web-data/results", "директория для готовых файлов (доступны для скачивания)")
	resultsTTLHours := flag.Int("results-ttl-hours", 2, "через сколько часов автоматически удалять готовые файлы (0 - никогда)")
	maxUploadMB := flag.Int("max-upload-mb", 25, "максимальный размер загружаемого файла в мегабайтах")
	flag.Parse()

	uploadsFsys, err := fs.NewFileSystem(*uploadsDir, *uploadsDir, *uploadsDir, *uploadsDir)
	if err != nil {
		log.Fatalf("Не удалось подготовить директорию загрузок: %v", err)
	}
	// resultsFsys используется только ради метода CleanProcessed - удобно
	// переиспользовать уже готовую функцию очистки по TTL, которая была
	// написана для консольной версии, вместо того чтобы писать вторую такую же.
	resultsFsys, err := fs.NewFileSystem(*resultsDir, *resultsDir, *resultsDir, *resultsDir)
	if err != nil {
		log.Fatalf("Не удалось подготовить директорию результатов: %v", err)
	}

	if *resultsTTLHours > 0 {
		go runCleanupLoop(resultsFsys, time.Duration(*resultsTTLHours)*time.Hour)
	}

	server := &webServer{
		uploadsFsys:  uploadsFsys,
		uploadsDir:   *uploadsDir,
		resultsDir:   *resultsDir,
		maxUploadMB:  *maxUploadMB,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", server.handleIndex)
	mux.HandleFunc("POST /api/convert", server.handleConvert)
	mux.HandleFunc("GET /api/download/{name}", server.handleDownload)

	log.Printf("Сайт запущен: http://localhost%s (или ваш адрес, если задан другой --addr)", *addr)
	if err := http.ListenAndServe(*addr, withLogging(mux)); err != nil {
		log.Fatalf("Сервер остановлен с ошибкой: %v", err)
	}
}

// runCleanupLoop периодически удаляет из resultsDir файлы старше ttl,
// чтобы диск не переполнялся результатами, которые никто не скачал.
// Работает, пока жив процесс - специально без graceful shutdown, чтобы не
// усложнять код: при остановке сервера горутина просто завершится вместе с ним.
func runCleanupLoop(resultsFsys *fs.FileSystem, ttl time.Duration) {
	interval := ttl / 2
	if interval < time.Minute {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		removed, err := resultsFsys.CleanProcessed(ttl)
		if err != nil {
			log.Printf("Автоочистка результатов завершилась с ошибками: %v", err)
		}
		if removed > 0 {
			log.Printf("Автоочистка результатов: удалено файлов старше %v - %d", ttl, removed)
		}
	}
}

// withLogging - простая middleware для логирования запросов.
func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s - %v", r.Method, r.URL.Path, time.Since(start))
	})
}

// webServer хранит зависимости HTTP-обработчиков.
type webServer struct {
	uploadsFsys *fs.FileSystem
	uploadsDir  string
	resultsDir  string
	maxUploadMB int
}

// handleIndex отдаёт страницу фронтенда, вшитую в бинарник через go:embed.
func (s *webServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "не удалось загрузить страницу", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

// handleConvert - основной эндпоинт: принимает файл и параметры обработки,
// вызывает CompressorService и отдаёт JSON со ссылкой на результат.
// Контракт запроса/ответа описан в комментарии внутри cmd/web/static/index.html.
func (s *webServer) handleConvert(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, int64(s.maxUploadMB)<<20)

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("файл слишком большой (максимум %d МБ) или запрос повреждён", s.maxUploadMB))
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "не удалось прочитать загруженный файл")
		return
	}
	defer file.Close()

	// Определяем формат исходника по расширению имени файла - как и в
	// консольной версии (см. buildJob в internal/infra/ui/ui.go).
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(header.Filename)), ".")
	srcFormat, err := imginfo.NewFormat(ext)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	reqID, err := newRequestID()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "не удалось создать идентификатор запроса")
		return
	}

	uploadPath := filepath.Join(s.uploadsDir, reqID+"."+ext)
	if err := saveUploadedFile(file, uploadPath); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "не удалось сохранить загруженный файл")
		return
	}
	defer os.Remove(uploadPath) // исходник нужен только на время обработки

	size, err := s.uploadsFsys.GetFileSize(uploadPath)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "не удалось получить размер файла")
		return
	}

	src, err := imginfo.NewSourceImage(uploadPath, srcFormat, size)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	target, err := s.buildTarget(r, reqID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	opts, err := s.buildProcessingOptions(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	comp := compressor.NewCompressorService(s.uploadsFsys)
	if err := comp.CompressImage(src, target, opts); err != nil {
		writeJSONError(w, statusForError(err), err.Error())
		return
	}

	sizeAfter, _ := s.uploadsFsys.GetFileSize(target.Path())
	resultName := filepath.Base(target.Path())

	writeJSON(w, http.StatusOK, map[string]any{
		"download_url": "/api/download/" + resultName,
		"filename":      downloadFilename(header.Filename, target.Format().Extension()),
		"size_before":   size,
		"size_after":    sizeAfter,
	})
}

// buildTarget разбирает поля формы, отвечающие за формат и качество, и
// собирает imginfo.TargetImage с путём в директории результатов.
func (s *webServer) buildTarget(r *http.Request, reqID string) (imginfo.TargetImage, error) {
	targetFormat, err := imginfo.NewFormat(r.FormValue("format"))
	if err != nil {
		return imginfo.TargetImage{}, err
	}

	quality := compression.DefaultQuality
	pngLevel := compression.DefaultCompressionLevel

	if targetFormat.Equals("png") {
		if v := r.FormValue("png_level"); v != "" {
			n, convErr := strconv.Atoi(v)
			if convErr != nil {
				return imginfo.TargetImage{}, apperrors.New(apperrors.TypeInvalidInput, "png_level должен быть числом")
			}
			pngLevel, err = compression.NewCompressionLevel(n)
			if err != nil {
				return imginfo.TargetImage{}, err
			}
		}
	} else {
		if v := r.FormValue("quality"); v != "" {
			n, convErr := strconv.Atoi(v)
			if convErr != nil {
				return imginfo.TargetImage{}, apperrors.New(apperrors.TypeInvalidInput, "quality должен быть числом")
			}
			quality, err = compression.NewQuality(n)
			if err != nil {
				return imginfo.TargetImage{}, err
			}
		}
	}

	resultPath := filepath.Join(s.resultsDir, reqID+"."+targetFormat.Extension())
	return imginfo.NewTargetImage(resultPath, quality, pngLevel, targetFormat)
}

// buildProcessingOptions разбирает поля формы, отвечающие за обрезку и водяной знак.
func (s *webServer) buildProcessingOptions(r *http.Request) (compressor.ProcessingOptions, error) {
	var opts compressor.ProcessingOptions

	switch r.FormValue("crop_mode") {
	case "", "none":
		// без обрезки

	case "manual":
		x, err1 := formInt(r, "crop_x")
		y, err2 := formInt(r, "crop_y")
		w, err3 := formInt(r, "crop_w")
		h, err4 := formInt(r, "crop_h")
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			return opts, apperrors.New(apperrors.TypeInvalidInput, "координаты обрезки должны быть целыми числами")
		}
		rect, err := imginfo.NewCropRect(x, y, w, h)
		if err != nil {
			return opts, err
		}
		opts.Crop = &compressor.CropSpec{Manual: &rect}

	case "preset":
		preset := imginfo.CropPreset(r.FormValue("crop_preset"))
		if !preset.IsValid() {
			return opts, apperrors.New(apperrors.TypeInvalidInput, "неизвестный пресет обрезки").WithContext("preset", string(preset))
		}
		opts.Crop = &compressor.CropSpec{Preset: preset}

	default:
		return opts, apperrors.New(apperrors.TypeInvalidInput, "неизвестный режим обрезки crop_mode")
	}

	if r.FormValue("watermark") == "1" {
		opacityPercent, err := formInt(r, "watermark_opacity")
		if err != nil {
			return opts, apperrors.New(apperrors.TypeInvalidInput, "watermark_opacity должен быть числом")
		}
		wm, err := imginfo.NewWatermarkOptions(
			r.FormValue("watermark_text"),
			imginfo.WatermarkPosition(r.FormValue("watermark_position")),
			float64(opacityPercent)/100,
		)
		if err != nil {
			return opts, err
		}
		opts.Watermark = &wm
	}

	return opts, nil
}

// handleDownload отдаёt готовый файл по его имени. Имя файла всегда
// сгенерировано сервером (см. newRequestID), поэтому filepath.Base
// достаточно, чтобы исключить выход за пределы resultsDir через "../".
func (s *webServer) handleDownload(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.PathValue("name"))
	path := filepath.Join(s.resultsDir, name)

	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "файл не найден или уже удалён", http.StatusNotFound)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", mimeForExt(filepath.Ext(name)))
	if _, err := io.Copy(w, f); err != nil {
		log.Printf("Ошибка при отдаче файла %s: %v", name, err)
	}
}

// ---------------------------------------------------------------------
// Вспомогательные функции
// ---------------------------------------------------------------------

// newRequestID генерирует короткий случайный идентификатор для имён файлов -
// не последовательный и не угадываемый, в отличие от, например, счётчика.
func newRequestID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// saveUploadedFile копирует содержимое загруженного файла на диск.
func saveUploadedFile(src io.Reader, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}
	dst, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return err
}

// formInt читает целое число из поля формы.
func formInt(r *http.Request, key string) (int, error) {
	return strconv.Atoi(r.FormValue(key))
}

// downloadFilename строит имя файла для скачивания на основе исходного имени.
func downloadFilename(originalName, targetExt string) string {
	base := strings.TrimSuffix(filepath.Base(originalName), filepath.Ext(originalName))
	base = strings.TrimSpace(base)
	if base == "" {
		base = "image"
	}
	return base + "-edited." + targetExt
}

// mimeForExt возвращает MIME-тип по расширению файла. Пишем свою маленькую
// таблицу вместо mime.TypeByExtension, потому что тип "image/webp"
// зарегистрирован не во всех сборках Go/ОС, а нам важно, чтобы браузер
// однозначно понимал, что отдаётся именно картинка.
func mimeForExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

// statusForError переводит тип AppError в подходящий HTTP-статус.
func statusForError(err error) int {
	appErr, ok := err.(*apperrors.AppError)
	if !ok {
		return http.StatusInternalServerError
	}
	switch appErr.Type {
	case apperrors.TypeInvalidInput, apperrors.TypeValidate, apperrors.TypeUnsupported:
		return http.StatusBadRequest
	case apperrors.TypeNotFound:
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

// writeJSON отправляет успешный JSON-ответ.
func writeJSON(w http.ResponseWriter, status int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("Ошибка кодирования JSON-ответа: %v", err)
	}
}

// writeJSONError отправляет JSON-ответ с ошибкой в формате {"error": "..."},
// как и ожидает фронтенд (см. контракт в cmd/web/static/index.html).
func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}
