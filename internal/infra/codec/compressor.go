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
	"path/filepath"

	"github.com/eringen/gowebper"
	"golang.org/x/image/webp"

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
// Для WebP используется чистая Go-библиотека golang.org/x/image/webp -
// ничего устанавливать в системе не нужно, всё работает "из коробки"
// после go mod tidy.
func (s *CompressorService) decodeImage(src imginfo.SourceImage) (image.Image, error) {
	data, err := s.fs.ReadFile(src.Path())
	if err != nil {
		return nil, err
	}

	if src.Format().Equals("webp") {
		img, err := webp.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, apperrors.WrapWithFile(
				err,
				apperrors.TypeDecode,
				"не удалось декодировать WebP изображение",
			).WithPath(src.Path())
		}
		return img, nil
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
// Используется чистая Go-библиотека github.com/eringen/gowebper: она кодирует
// в формат VP8L (WebP lossless) без cgo и без системных зависимостей вроде
// libwebp/cwebp - достаточно "go mod tidy". Значение quality (1-100) управляет
// параметром Options.Quality библиотеки: это не совсем то же самое, что
// "качество" в классическом lossy JPEG, а управление предварительным
// квантованием цвета перед lossless-упаковкой (чем меньше quality - тем
// сильнее огрубление цвета и меньше файл, при 100 - почти без потерь).
// Итоговый файл в любом случае остаётся корректным WebP, читается браузерами
// и любым просмотрщиком WebP.
func (s *CompressorService) encodeWebP(img image.Image, path string, quality compression.Quality) error {
	out, err := os.Create(path)
	if err != nil {
		return apperrors.WrapWithFile(err, apperrors.TypeIO, "не удалось создать файл результата").WithPath(path)
	}
	defer out.Close()

	opts := &gowebper.Options{
		Level:   gowebper.LevelDefault,
		Quality: quality.Value(),
	}
	if err := gowebper.Encode(out, img, opts); err != nil {
		return apperrors.WrapWithFile(err, apperrors.TypeEncode, "не удалось закодировать WebP").WithContext("quality", quality.Value())
	}
	return nil
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
