// Package ui - простой консольный интерфейс приложения.
//
// Специально устроен так, чтобы вся "настоящая" логика (сжатие, обрезка,
// водяной знак, параллельная обработка) не зависела от консоли - она лежит
// в internal/infra/codec и internal/app/pipeline. ConsoleUI лишь опрашивает
// пользователя и вызывает эти сервисы. Когда появится сайт, для него нужно
// будет написать другой "интерфейс" (HTTP-обработчики) поверх тех же
// сервисов, а этот файл можно будет не трогать.
package ui

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QtaroAXE/image-redactor/internal/app/pipeline"
	"github.com/QtaroAXE/image-redactor/internal/config"
	"github.com/QtaroAXE/image-redactor/internal/domain/compression"
	"github.com/QtaroAXE/image-redactor/internal/domain/imginfo"
	compressor "github.com/QtaroAXE/image-redactor/internal/infra/codec"
	"github.com/QtaroAXE/image-redactor/internal/infra/fs"
)

// ConsoleUI - консольный интерфейс приложения.
type ConsoleUI struct {
	cfg    *config.Config
	reader *bufio.Reader
}

// NewConsoleUI создаёт новый консольный интерфейс.
func NewConsoleUI(cfg *config.Config) *ConsoleUI {
	return &ConsoleUI{cfg: cfg, reader: bufio.NewReader(os.Stdin)}
}

// Run запускает главное меню и работает, пока пользователь не выберет выход.
func (ui *ConsoleUI) Run() error {
	ui.printHeader()

	for {
		ui.printMenu()
		choice := ui.getInput("Выберите пункт меню: ")

		switch choice {
		case "1":
			ui.processAll()
		case "2":
			ui.processSingle()
		case "3":
			ui.showConfig()
		case "4":
			ui.changeConfig()
		case "5":
			ui.cleanProcessed()
		case "6":
			ui.CleanupOnExit()
			fmt.Println("\nДо свидания!")
			return nil
		default:
			fmt.Println("Неверный пункт меню, попробуйте снова.")
		}
	}
}

// printHeader печатает заголовок программы.
func (ui *ConsoleUI) printHeader() {
	fmt.Println("========================================")
	fmt.Println("   РЕДАКТОР ИЗОБРАЖЕНИЙ - консольная утилита")
	fmt.Println("========================================")
}

// printMenu печатает главное меню.
func (ui *ConsoleUI) printMenu() {
	fmt.Println("\nГЛАВНОЕ МЕНЮ:")
	fmt.Println("  1. Обработать все файлы из входной директории")
	fmt.Println("  2. Обработать один файл")
	fmt.Println("  3. Показать текущие настройки")
	fmt.Println("  4. Изменить настройки")
	fmt.Println("  5. Очистить директорию processed")
	fmt.Println("  6. Выход")
}

// getInput выводит подсказку и считывает строку, введённую пользователем.
func (ui *ConsoleUI) getInput(prompt string) string {
	fmt.Print(prompt)
	input, _ := ui.reader.ReadString('\n')
	return strings.TrimSpace(input)
}

// getInputDefault - то же самое, что getInput, но при пустом вводе
// возвращает значение по умолчанию.
func (ui *ConsoleUI) getInputDefault(prompt, def string) string {
	val := ui.getInput(fmt.Sprintf("%s [%s]: ", prompt, def))
	if val == "" {
		return def
	}
	return val
}

// ---------------------------------------------------------------------
// Обработка файлов
// ---------------------------------------------------------------------

// processAll обрабатывает все файлы из входной директории с одинаковыми
// настройками (формат, обрезка, водяной знак), которые запрашиваются один раз.
func (ui *ConsoleUI) processAll() {
	fmt.Println("\n--- Обработка всех файлов ---")

	fsys, err := fs.NewFileSystem(ui.cfg.Input, ui.cfg.Output, ui.cfg.Processed, ui.cfg.Errors)
	if err != nil {
		fmt.Printf("Ошибка создания файловой системы: %v\n", err)
		return
	}

	files, err := fsys.GetInputFiles()
	if err != nil {
		fmt.Printf("Ошибка получения списка файлов: %v\n", err)
		return
	}
	if len(files) == 0 {
		fmt.Printf("Во входной директории нет файлов: %s\n", ui.cfg.Input)
		return
	}
	fmt.Printf("Найдено файлов: %d\n", len(files))

	settings := ui.askProcessingSettings()

	comp := compressor.NewCompressorService(fsys)
	pool := pipeline.NewWorkerPool(comp, fsys, pipeline.PoolConfig{WorkerCount: ui.cfg.Workers})
	pool.Start()

	var progressWG sync.WaitGroup
	stopProgress := make(chan struct{})
	progressWG.Add(1)
	go ui.runProgressBar(pool, stopProgress, &progressWG)

	added := 0
	for _, path := range files {
		src, target, err := ui.buildJob(path, fsys, settings)
		if err != nil {
			fmt.Printf("\nПропуск файла %s: %v\n", path, err)
			continue
		}
		if err := pool.AddJob(src, target, settings.options()); err != nil {
			fmt.Printf("\nНе удалось добавить задачу для %s: %v\n", path, err)
			continue
		}
		added++
	}

	pool.Wait()
	close(stopProgress)
	progressWG.Wait()
	pool.Stop()

	stats := pool.GetStats()
	fmt.Println("\n\nГотово!")
	fmt.Printf("Добавлено в обработку: %d, успешно: %d, с ошибкой: %d, повторов: %d\n",
		added, stats.CompletedJobs, stats.FailedJobs, stats.RetriedJobs)
}

// processSingle обрабатывает один файл, путь к которому вводит пользователь.
func (ui *ConsoleUI) processSingle() {
	fmt.Println("\n--- Обработка одного файла ---")

	path := ui.getInput("Путь к файлу: ")
	if _, err := os.Stat(path); err != nil {
		fmt.Printf("Файл не найден: %s\n", path)
		return
	}

	fsys, err := fs.NewFileSystem(ui.cfg.Input, ui.cfg.Output, ui.cfg.Processed, ui.cfg.Errors)
	if err != nil {
		fmt.Printf("Ошибка создания файловой системы: %v\n", err)
		return
	}

	settings := ui.askProcessingSettings()

	src, target, err := ui.buildJob(path, fsys, settings)
	if err != nil {
		fmt.Printf("Ошибка подготовки файла: %v\n", err)
		return
	}

	fmt.Printf("Источник: %s\n", src.Path())
	fmt.Printf("Результат: %s\n", target.Path())

	comp := compressor.NewCompressorService(fsys)
	start := time.Now()
	err = comp.CompressImage(src, target, settings.options())
	duration := time.Since(start)

	if err != nil {
		fmt.Printf("Ошибка обработки: %v\n", err)
		fsys.MoveToError(path)
		return
	}

	sizeBefore, _ := fsys.GetFileSize(path)
	sizeAfter, _ := fsys.GetFileSize(target.Path())
	reduction := 0.0
	if sizeBefore > 0 {
		reduction = (1 - float64(sizeAfter)/float64(sizeBefore)) * 100
	}

	fmt.Println("\nГотово!")
	fmt.Printf("Размер до: %d байт\n", sizeBefore)
	fmt.Printf("Размер после: %d байт\n", sizeAfter)
	fmt.Printf("Сжатие: %.2f%%\n", reduction)
	fmt.Printf("Время: %v\n", duration)

	fsys.MoveToProcessed(path)
}

// runProgressBar периодически опрашивает статистику пула и печатает
// текстовый прогресс-бар в одной строке, пока не придёт сигнал остановки.
func (ui *ConsoleUI) runProgressBar(pool *pipeline.WorkerPool, stop <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	print := func() {
		stats := pool.GetStats()
		done := stats.CompletedJobs + stats.FailedJobs
		printProgressBar(done, stats.TotalJobs)
	}

	for {
		select {
		case <-stop:
			print() // финальное состояние перед выходом
			return
		case <-ticker.C:
			print()
		}
	}
}

// printProgressBar печатает прогресс-бар вида: [#####-----] 50% (5/10)
// поверх текущей строки консоли (с помощью возврата каретки \r).
func printProgressBar(done, total int) {
	const width = 30
	percent := 0.0
	if total > 0 {
		percent = float64(done) / float64(total) * 100
	}
	filled := int(float64(width) * percent / 100)
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("#", filled) + strings.Repeat("-", width-filled)
	fmt.Printf("\rПрогресс: [%s] %5.1f%% (%d/%d)", bar, percent, done, total)
}

// ---------------------------------------------------------------------
// Настройки, конфиг
// ---------------------------------------------------------------------

// showConfig показывает текущую конфигурацию.
func (ui *ConsoleUI) showConfig() {
	fmt.Println("\nТекущие настройки:")
	fmt.Printf("  Входная директория: %s\n", ui.cfg.Input)
	fmt.Printf("  Выходная директория: %s\n", ui.cfg.Output)
	fmt.Printf("  Директория обработанных файлов: %s\n", ui.cfg.Processed)
	fmt.Printf("  Директория ошибок: %s\n", ui.cfg.Errors)
	fmt.Printf("  Количество воркеров: %d\n", ui.cfg.Workers)
	fmt.Printf("  Качество по умолчанию: %d\n", ui.cfg.Quality)
	if ui.cfg.ProcessedTTLHours > 0 {
		fmt.Printf("  Автоочистка processed при выходе: включена, старше %d ч.\n", ui.cfg.ProcessedTTLHours)
	} else {
		fmt.Println("  Автоочистка processed при выходе: отключена")
	}
}

// changeConfig позволяет изменить и сохранить конфигурацию.
func (ui *ConsoleUI) changeConfig() {
	fmt.Println("\nИзменение настроек (пустой ввод - оставить как есть)")

	ui.cfg.Input = ui.getInputDefault("Входная директория", ui.cfg.Input)
	ui.cfg.Output = ui.getInputDefault("Выходная директория", ui.cfg.Output)
	ui.cfg.Processed = ui.getInputDefault("Директория обработанных файлов", ui.cfg.Processed)
	ui.cfg.Errors = ui.getInputDefault("Директория ошибок", ui.cfg.Errors)

	if v := ui.getInput(fmt.Sprintf("Количество воркеров [%d]: ", ui.cfg.Workers)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			ui.cfg.Workers = n
		} else {
			fmt.Println("Некорректное значение, оставляю прежнее.")
		}
	}

	if v := ui.getInput(fmt.Sprintf("Качество по умолчанию (1-100) [%d]: ", ui.cfg.Quality)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 100 {
			ui.cfg.Quality = n
		} else {
			fmt.Println("Некорректное значение, оставляю прежнее.")
		}
	}

	if v := ui.getInput(fmt.Sprintf("Автоочистка processed при выходе, часов (0 - отключить) [%d]: ", ui.cfg.ProcessedTTLHours)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			ui.cfg.ProcessedTTLHours = n
		} else {
			fmt.Println("Некорректное значение, оставляю прежнее.")
		}
	}

	if strings.ToLower(ui.getInput("Сохранить настройки в configs/config.json? (y/n): ")) == "y" {
		if err := config.SaveConfig("configs/config.json", ui.cfg); err != nil {
			fmt.Printf("Ошибка сохранения: %v\n", err)
		} else {
			fmt.Println("Настройки сохранены в configs/config.json")
		}
	}
}

// cleanProcessed - пункт меню для ручной очистки директории processed.
// Пользователь сам решает: удалить всё или только файлы старше N часов.
func (ui *ConsoleUI) cleanProcessed() {
	fmt.Println("\n--- Очистка директории processed ---")
	fmt.Printf("Директория: %s\n", ui.cfg.Processed)

	fmt.Println("Что удалить?")
	fmt.Println("  1. Все файлы")
	fmt.Println("  2. Только файлы старше N часов")

	var maxAge time.Duration
	switch ui.getInputDefault("Выбор", "1") {
	case "2":
		hours := ui.askInt("Старше скольких часов удалять", 24)
		maxAge = time.Duration(hours) * time.Hour
	default:
		maxAge = 0 // 0 означает "удалить всё", см. CleanProcessed
	}

	fsys, err := fs.NewFileSystem(ui.cfg.Input, ui.cfg.Output, ui.cfg.Processed, ui.cfg.Errors)
	if err != nil {
		fmt.Printf("Ошибка создания файловой системы: %v\n", err)
		return
	}

	removed, err := fsys.CleanProcessed(maxAge)
	if err != nil {
		fmt.Printf("Удалено файлов: %d, но были ошибки: %v\n", removed, err)
		return
	}
	fmt.Printf("Удалено файлов: %d\n", removed)
}

// CleanupOnExit выполняется автоматически при выходе из приложения -
// как через пункт меню "Выход", так и по сигналу завершения (Ctrl+C),
// см. main.go. Если в конфиге задан ProcessedTTLHours (> 0), файлы в
// processed старше этого срока удаляются молча. Если ProcessedTTLHours == 0
// (значение по умолчанию) - автоочистка отключена, и функция ничего не
// делает, чтобы поведение приложения было предсказуемым и не удаляло файлы
// без явного согласия пользователя в конфиге.
func (ui *ConsoleUI) CleanupOnExit() {
	if ui.cfg.ProcessedTTLHours <= 0 {
		return
	}

	fsys, err := fs.NewFileSystem(ui.cfg.Input, ui.cfg.Output, ui.cfg.Processed, ui.cfg.Errors)
	if err != nil {
		return // при выходе не хотим пугать пользователя ошибками файловой системы
	}

	maxAge := time.Duration(ui.cfg.ProcessedTTLHours) * time.Hour
	removed, err := fsys.CleanProcessed(maxAge)
	if removed > 0 {
		fmt.Printf("Автоочистка processed: удалено файлов старше %d ч. - %d\n", ui.cfg.ProcessedTTLHours, removed)
	}
	if err != nil {
		fmt.Printf("Автоочистка processed завершилась с ошибками: %v\n", err)
	}
}

// ---------------------------------------------------------------------
// Настройки обработки: формат, качество, обрезка, водяной знак
// ---------------------------------------------------------------------

// processingSettings - настройки, которые пользователь выбирает перед
// запуском обработки (общие для всех файлов в рамках одного запуска).
type processingSettings struct {
	format     imginfo.Format
	quality    compression.Quality
	pngLevel   compression.CompressionLevel
	crop       *compressor.CropSpec
	watermark  *imginfo.WatermarkOptions
}

// options собирает crop/watermark в ProcessingOptions для передачи в компрессор.
func (s processingSettings) options() compressor.ProcessingOptions {
	return compressor.ProcessingOptions{Crop: s.crop, Watermark: s.watermark}
}

// askProcessingSettings опрашивает пользователя обо всех параметрах обработки.
func (ui *ConsoleUI) askProcessingSettings() processingSettings {
	var s processingSettings

	s.format = ui.askFormat()

	switch s.format.String() {
	case "png":
		s.pngLevel = ui.askCompressionLevel()
	default: // jpeg, webp
		s.quality = ui.askQuality()
	}

	s.crop = ui.askCrop()
	s.watermark = ui.askWatermark()

	return s
}

// askFormat запрашивает у пользователя целевой формат изображения.
func (ui *ConsoleUI) askFormat() imginfo.Format {
	for {
		fmt.Println("\nВ какой формат конвертировать?")
		fmt.Println("  1. JPG")
		fmt.Println("  2. PNG")
		fmt.Println("  3. WebP")
		switch ui.getInputDefault("Выбор", "1") {
		case "1":
			return imginfo.FormatJPEG
		case "2":
			return imginfo.FormatPNG
		case "3":
			return imginfo.FormatWebP
		default:
			fmt.Println("Некорректный выбор, попробуйте снова.")
		}
	}
}

// askQuality запрашивает качество сжатия (для JPEG/WebP).
func (ui *ConsoleUI) askQuality() compression.Quality {
	for {
		v := ui.getInputDefault("Качество (1-100)", strconv.Itoa(ui.cfg.Quality))
		n, err := strconv.Atoi(v)
		if err != nil {
			fmt.Println("Введите целое число.")
			continue
		}
		q, err := compression.NewQuality(n)
		if err != nil {
			fmt.Printf("Некорректное значение: %v\n", err)
			continue
		}
		return q
	}
}

// askCompressionLevel запрашивает уровень сжатия (для PNG).
func (ui *ConsoleUI) askCompressionLevel() compression.CompressionLevel {
	for {
		v := ui.getInputDefault("Уровень сжатия PNG (1-10)", "6")
		n, err := strconv.Atoi(v)
		if err != nil {
			fmt.Println("Введите целое число.")
			continue
		}
		lvl, err := compression.NewCompressionLevel(n)
		if err != nil {
			fmt.Printf("Некорректное значение: %v\n", err)
			continue
		}
		return lvl
	}
}

// askCrop спрашивает, нужно ли обрезать фото, и если да - вручную или пресетом.
func (ui *ConsoleUI) askCrop() *compressor.CropSpec {
	fmt.Println("\nОбрезать изображение?")
	fmt.Println("  1. Не обрезать")
	fmt.Println("  2. Обрезать по координатам")
	fmt.Println("  3. Обрезать по готовому формату")

	switch ui.getInputDefault("Выбор", "1") {
	case "2":
		x := ui.askInt("X (левый верхний угол)", 0)
		y := ui.askInt("Y (левый верхний угол)", 0)
		w := ui.askInt("Ширина", 0)
		h := ui.askInt("Высота", 0)
		rect, err := imginfo.NewCropRect(x, y, w, h)
		if err != nil {
			fmt.Printf("Некорректные координаты обрезки, обрезка отменена: %v\n", err)
			return nil
		}
		return &compressor.CropSpec{Manual: &rect}
	case "3":
		fmt.Println("  1. Квадрат 1:1 (например, аватар)")
		fmt.Println("  2. Портрет 4:5")
		fmt.Println("  3. Сторис 9:16")
		fmt.Println("  4. Широкоформатное 16:9")
		var preset imginfo.CropPreset
		switch ui.getInputDefault("Выбор пресета", "1") {
		case "1":
			preset = imginfo.CropPresetSquare
		case "2":
			preset = imginfo.CropPresetPortrait
		case "3":
			preset = imginfo.CropPresetStory
		case "4":
			preset = imginfo.CropPresetWidescreen
		default:
			fmt.Println("Некорректный выбор, обрезка отменена.")
			return nil
		}
		return &compressor.CropSpec{Preset: preset}
	default:
		return nil
	}
}

// askWatermark спрашивает, нужно ли добавить водяной знак, и его параметры.
func (ui *ConsoleUI) askWatermark() *imginfo.WatermarkOptions {
	if strings.ToLower(ui.getInputDefault("Добавить водяной знак? (y/n)", "n")) != "y" {
		return nil
	}

	text := ui.getInput("Текст водяного знака: ")

	fmt.Println("Расположение:")
	fmt.Println("  1. Верхний левый угол")
	fmt.Println("  2. Верхний правый угол")
	fmt.Println("  3. Нижний левый угол")
	fmt.Println("  4. Нижний правый угол")
	fmt.Println("  5. По центру")

	var pos imginfo.WatermarkPosition
	switch ui.getInputDefault("Выбор", "4") {
	case "1":
		pos = imginfo.WatermarkTopLeft
	case "2":
		pos = imginfo.WatermarkTopRight
	case "3":
		pos = imginfo.WatermarkBottomLeft
	case "5":
		pos = imginfo.WatermarkCenter
	default:
		pos = imginfo.WatermarkBottomRight
	}

	opacityPercent := ui.askInt("Прозрачность в % (100 - полностью видимый)", 50)
	opacity := float64(opacityPercent) / 100

	opts, err := imginfo.NewWatermarkOptions(text, pos, opacity)
	if err != nil {
		fmt.Printf("Некорректные параметры водяного знака, знак не будет добавлен: %v\n", err)
		return nil
	}
	return &opts
}

// askInt запрашивает у пользователя целое число с значением по умолчанию.
func (ui *ConsoleUI) askInt(prompt string, def int) int {
	v := ui.getInputDefault(prompt, strconv.Itoa(def))
	n, err := strconv.Atoi(v)
	if err != nil {
		fmt.Println("Введено не число, использую значение по умолчанию.")
		return def
	}
	return n
}

// buildJob собирает SourceImage и TargetImage для конкретного файла на основе
// выбранных пользователем настроек обработки.
func (ui *ConsoleUI) buildJob(path string, fsys *fs.FileSystem, settings processingSettings) (imginfo.SourceImage, imginfo.TargetImage, error) {
	ext := filepath.Ext(path)
	formatName := strings.TrimPrefix(ext, ".")

	srcFormat, err := imginfo.NewFormat(formatName)
	if err != nil {
		return imginfo.SourceImage{}, imginfo.TargetImage{}, err
	}

	size, err := fsys.GetFileSize(path)
	if err != nil {
		return imginfo.SourceImage{}, imginfo.TargetImage{}, err
	}

	src, err := imginfo.NewSourceImage(path, srcFormat, size)
	if err != nil {
		return imginfo.SourceImage{}, imginfo.TargetImage{}, err
	}

	quality := settings.quality
	if quality.IsZero() {
		quality = compression.DefaultQuality
	}
	level := settings.pngLevel
	if level.IsZero() {
		level = compression.DefaultCompressionLevel
	}

	outputPath := fsys.GetOutputPath(path, settings.format.Extension())
	target, err := imginfo.NewTargetImage(outputPath, quality, level, settings.format)
	if err != nil {
		return imginfo.SourceImage{}, imginfo.TargetImage{}, err
	}
	return src, target, nil
}
