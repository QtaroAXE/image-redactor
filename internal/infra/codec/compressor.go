// Package compressor - основная бизнес-логика обработки одного изображения:
// декодирование, обрезка, водяной знак, сжатие и сохранение в нужном формате.
// Специально не зависит от консольного интерфейса (internal/infra/ui) и от
// пула воркеров (internal/app/pipeline) - только от файловой системы и доменных
// моделей. Это сделано намеренно: в будущем эту же логику можно будет вызвать
// из HTTP-обработчика сайта, просто передав туда файл и ProcessingOptions.
package compressor

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/QtaroAXE/image-redactor/internal/domain/compression"
	apperrors "github.com/QtaroAXE/image-redactor/internal/domain/errors"
	"github.com/QtaroAXE/image-redactor/internal/domain/imginfo"
	"github.com/QtaroAXE/image-redactor/internal/infra/fs"
)

// CompressorService - сервис сжатия/конвертации изображений.
type CompressorService struct {
	fs *fs.FileSystem
}

// NewCompressorService создаёт сервис сжатия.
func NewCompressorService(fs *fs.FileSystem) *CompressorService {
	return &CompressorService{fs: fs}
}

// CropSpec описывает, как обрезать изображение: либо точные координаты
// (Manual), либо один из заготовленных пресетов (Preset). Если оба поля
// пустые - обрезка не выполняется.
type CropSpec struct {
	Manual *imginfo.CropRect
	Preset imginfo.CropPreset
}

// ProcessingOptions - все необязательные шаги обработки одного изображения,
// которые применяются перед сохранением в целевом формате.
type ProcessingOptions struct {
	Crop      *CropSpec
	Watermark *imginfo.WatermarkOptions
}

// CompressImage - главный метод: читает исходный файл, применяет обрезку и
// водяной знак (если заданы), кодирует в целевой формат и сохраняет на диск.
func (s *CompressorService) CompressImage(src imginfo.SourceImage, target imginfo.TargetImage, opts ProcessingOptions) error {
	img, err := s.decodeImage(src)
	if err != nil {
		return err
	}

	if img.Bounds().Dx() == 0 || img.Bounds().Dy() == 0 {
		return apperrors.NewWithFile(
			apperrors.TypeValidate,
			"изображение имеет нулевые размеры",
		).WithPath(src.Path())
	}

	if opts.Crop != nil {
		img, err = s.applyCrop(img, *opts.Crop)
		if err != nil {
			return err
		}
	}

	if opts.Watermark != nil {
		img = ApplyWatermark(img, *opts.Watermark)
	}

	if err := os.MkdirAll(filepath.Dir(target.Path()), 0755); err != nil {
		return apperrors.WrapWithFile(
			err,
			apperrors.TypeInternal,
			"не удалось создать директорию для результата",
		).WithPath(target.Path())
	}

	switch target.Format().String() {
	case "jpeg":
		return s.encodeJPEG(img, target.Path(), target.Quality())
	case "png":
		return s.encodePNG(img, target.Path(), target.CompressionLevel())
	case "webp":
		return s.encodeWebP(img, target.Path(), target.Quality())
	default:
		return apperrors.NewWithFile(
			apperrors.TypeUnsupported,
			fmt.Sprintf("неподдерживаемый формат: %s", target.Format()),
		).WithContext("format", target.Format())
	}
}

// decodeImage читает файл источника и декодирует его в image.Image.
// Для WebP используется системная утилита dwebp (см. комментарий у encodeWebP),
// так как в стандартной библиотеке Go декодера WebP нет, а сторонние Go-пакеты
// мы намеренно не подключаем.
func (s *CompressorService) decodeImage(src imginfo.SourceImage) (image.Image, error) {
	if src.Format().Equals("webp") {
		return s.decodeWebPViaDwebp(src.Path())
	}

	data, err := s.fs.ReadFile(src.Path())
	if err != nil {
		return nil, err
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, apperrors.WrapWithFile(
			err,
			apperrors.TypeDecode,
			"не удалось декодировать изображение",
		).WithPath(src.Path())
	}
	return img, nil
}

// applyCrop вычисляет итоговый прямоугольник обрезки (из ручных координат
// либо из пресета) и вызывает CropImage.
func (s *CompressorService) applyCrop(img image.Image, spec CropSpec) (image.Image, error) {
	if spec.Manual != nil {
		return CropImage(img, *spec.Manual), nil
	}
	if spec.Preset != "" {
		bounds := img.Bounds()
		rect, err := spec.Preset.RectForImage(bounds.Dx(), bounds.Dy())
		if err != nil {
			return nil, err
		}
		return CropImage(img, rect), nil
	}
	return img, nil
}

// encodeJPEG сохраняет изображение в формате JPEG с заданным качеством.
func (s *CompressorService) encodeJPEG(img image.Image, path string, quality compression.Quality) error {
	out, err := os.Create(path)
	if err != nil {
		return apperrors.WrapWithFile(err, apperrors.TypeIO, "не удалось создать файл результата").WithPath(path)
	}
	defer out.Close()

	opts := &jpeg.Options{Quality: quality.Value()}
	if err := jpeg.Encode(out, img, opts); err != nil {
		return apperrors.WrapWithFile(err, apperrors.TypeEncode, "не удалось закодировать JPEG").WithContext("quality", quality.Value())
	}
	return nil
}

// encodePNG сохраняет изображение в формате PNG с заданным уровнем сжатия.
func (s *CompressorService) encodePNG(img image.Image, path string, level compression.CompressionLevel) error {
	out, err := os.Create(path)
	if err != nil {
		return apperrors.WrapWithFile(err, apperrors.TypeIO, "не удалось создать файл результата").WithPath(path)
	}
	defer out.Close()

	encoder := &png.Encoder{CompressionLevel: mapCompressionLevel(level.Value())}
	if err := encoder.Encode(out, img); err != nil {
		return apperrors.WrapWithFile(err, apperrors.TypeEncode, "не удалось закодировать PNG").WithContext("level", level.Value())
	}
	return nil
}

// encodeWebP сохраняет изображение в формате WebP.
//
// В стандартной библиотеке Go нет кодировщика WebP, а полноценная реализация
// формата WebP с нуля (энтропийное кодирование, предсказания и т.д.) - это
// тысячи строк кода, которые невозможно надёжно написать и проверить без
// возможности собрать и прогнать проект. Поэтому вместо стороннего Go-пакета
// с GitHub мы используем системную консольную утилиту cwebp из пакета libwebp:
// она ставится через менеджер пакетов ОС (не через "go get"), например:
//
//	Ubuntu/Debian: sudo apt install webp
//	macOS (Homebrew): brew install webp
//	Windows: скачать сборку с https://developers.google.com/speed/webp/download
//
// Если утилита не установлена - функция вернёт понятную ошибку, а не панику.
func (s *CompressorService) encodeWebP(img image.Image, path string, quality compression.Quality) error {
	if _, err := exec.LookPath("cwebp"); err != nil {
		return apperrors.New(
			apperrors.TypeUnsupported,
			"для сохранения в WebP требуется установленная утилита cwebp (пакет libwebp); используйте формат JPEG или PNG, либо установите libwebp",
		)
	}

	tmpPNG, err := os.CreateTemp("", "image-redactor-*.png")
	if err != nil {
		return apperrors.WrapWithFile(err, apperrors.TypeInternal, "не удалось создать временный файл")
	}
	tmpPath := tmpPNG.Name()
	defer os.Remove(tmpPath)

	if err := png.Encode(tmpPNG, img); err != nil {
		tmpPNG.Close()
		return apperrors.WrapWithFile(err, apperrors.TypeEncode, "не удалось подготовить временный PNG для WebP")
	}
	tmpPNG.Close()

	cmd := exec.Command("cwebp", "-quiet", "-q", fmt.Sprintf("%d", quality.Value()), tmpPath, "-o", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return apperrors.WrapWithFile(
			fmt.Errorf("%s: %s", err, string(out)),
			apperrors.TypeEncode,
			"утилита cwebp завершилась с ошибкой",
		).WithPath(path)
	}
	return nil
}

// decodeWebPViaDwebp конвертирует WebP-файл во временный PNG с помощью
// системной утилиты dwebp (тот же пакет libwebp, что и cwebp) и декодирует
// результат стандартными средствами Go.
func (s *CompressorService) decodeWebPViaDwebp(path string) (image.Image, error) {
	if _, err := exec.LookPath("dwebp"); err != nil {
		return nil, apperrors.New(
			apperrors.TypeUnsupported,
			"для чтения WebP-файлов требуется установленная утилита dwebp (пакет libwebp)",
		).WithPath(path)
	}

	tmpPNG, err := os.CreateTemp("", "image-redactor-src-*.png")
	if err != nil {
		return nil, apperrors.WrapWithFile(err, apperrors.TypeInternal, "не удалось создать временный файл")
	}
	tmpPath := tmpPNG.Name()
	tmpPNG.Close()
	defer os.Remove(tmpPath)

	cmd := exec.Command("dwebp", "-quiet", path, "-o", tmpPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, apperrors.WrapWithFile(
			fmt.Errorf("%s: %s", err, string(out)),
			apperrors.TypeDecode,
			"утилита dwebp завершилась с ошибкой",
		).WithPath(path)
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, apperrors.WrapWithFile(err, apperrors.TypeIO, "не удалось прочитать результат dwebp")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, apperrors.WrapWithFile(err, apperrors.TypeDecode, "не удалось декодировать результат dwebp")
	}
	return img, nil
}

// mapCompressionLevel переводит уровень сжатия 1..10 в шкалу png.CompressionLevel.
func mapCompressionLevel(level int) png.CompressionLevel {
	switch {
	case level <= 1:
		return png.BestSpeed
	case level >= 9:
		return png.BestCompression
	default:
		return png.CompressionLevel(level - 1)
	}
}
