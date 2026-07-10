package compressor

import "image"

// ResizeImage изменяет размер изображения простым методом "ближайшего соседа".
// Метод не такой гладкий, как билинейная интерполяция, зато прост и понятен
// и не требует сторонних библиотек (golang.org/x/image/draw и т.п.).
// Если width или height равны 0, соответствующая сторона считается
// автоматически с сохранением пропорций.
func ResizeImage(img image.Image, width, height int) image.Image {
	srcBounds := img.Bounds()
	srcWidth := srcBounds.Dx()
	srcHeight := srcBounds.Dy()

	if width == 0 && height == 0 {
		return img
	}
	if width > 0 && height == 0 {
		height = int(float64(srcHeight) * float64(width) / float64(srcWidth))
	} else if height > 0 && width == 0 {
		width = int(float64(srcWidth) * float64(height) / float64(srcHeight))
	}
	if width <= 0 {
		width = srcWidth
	}
	if height <= 0 {
		height = srcHeight
	}

	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		srcY := srcBounds.Min.Y + y*srcHeight/height
		for x := 0; x < width; x++ {
			srcX := srcBounds.Min.X + x*srcWidth/width
			dst.Set(x, y, img.At(srcX, srcY))
		}
	}
	return dst
}
