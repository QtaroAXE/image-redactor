package compressor

import (
	"image"
	"image/draw"

	"github.com/QtaroAXE/image-redactor/internal/domain/imginfo"
)

// CropImage обрезает изображение по прямоугольнику rect.
// Если rect выходит за границы изображения, он автоматически подгоняется
// (обрезается по границе) - это удобно при пакетной обработке файлов
// разного размера с одинаковыми координатами обрезки.
func CropImage(img image.Image, rect imginfo.CropRect) image.Image {
	bounds := img.Bounds()
	clamped := rect.ClampTo(bounds.Dx(), bounds.Dy())

	if clamped.Width <= 0 || clamped.Height <= 0 {
		// Обрезка выродилась в пустую область (например, координаты были
		// сильно за пределами фото) - возвращаем изображение без изменений,
		// чтобы не терять файл целиком.
		return img
	}

	srcRect := image.Rect(
		bounds.Min.X+clamped.X,
		bounds.Min.Y+clamped.Y,
		bounds.Min.X+clamped.X+clamped.Width,
		bounds.Min.Y+clamped.Y+clamped.Height,
	)

	// Если исходное изображение поддерживает быструю SubImage - используем её.
	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	if si, ok := img.(subImager); ok {
		return si.SubImage(srcRect)
	}

	// Иначе копируем нужную область вручную в новое изображение.
	dst := image.NewRGBA(image.Rect(0, 0, clamped.Width, clamped.Height))
	draw.Draw(dst, dst.Bounds(), img, srcRect.Min, draw.Src)
	return dst
}
