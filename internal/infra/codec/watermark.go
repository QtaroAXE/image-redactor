package compressor

import (
	"image"
	"image/color"
	"image/draw"
	"strings"

	"github.com/QtaroAXE/image-redactor/internal/domain/imginfo"
)

// pixelScale - во сколько раз увеличивается один "пиксель" шрифта.
const pixelScale = 4

// letterSpacing - расстояние между буквами в пикселях шрифта (до масштабирования).
const letterSpacing = 1

// margin - отступ водяного знака от края фотографии в пикселях.
const watermarkMargin = 20

// renderWatermarkText рисует текст водяного знака на прозрачном холсте
// и возвращает готовую картинку
func renderWatermarkText(text string, opacity float64) *image.RGBA {
	text = strings.ToUpper(text)
	runes := []rune(text)

	cols := 0
	for range runes {
		cols += glyphWidth + letterSpacing
	}
	width := cols * pixelScale
	height := glyphHeight * pixelScale
	if width <= 0 {
		width = 1
	}

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	alpha := uint8(opacity * 255)
	textColor := color.RGBA{R: 255, G: 255, B: 255, A: alpha}

	col := 0
	for _, r := range runes {
		g := glyphFor(r)
		for row := 0; row < glyphHeight; row++ {
			bits := g[row]
			for c := 0; c < glyphWidth; c++ {
				if bits&(1<<uint(glyphWidth-1-c)) == 0 {
					continue
				}
				x0 := (col + c) * pixelScale
				y0 := row * pixelScale
				for dy := 0; dy < pixelScale; dy++ {
					for dx := 0; dx < pixelScale; dx++ {
						canvas.Set(x0+dx, y0+dy, textColor)
					}
				}
			}
		}
		col += glyphWidth + letterSpacing
	}

	return canvas
}

// ApplyWatermark накладывает полупрозрачный текстовый водяной знак на изображение
// и возвращает новое изображение (исходное не изменяется).
func ApplyWatermark(img image.Image, opts imginfo.WatermarkOptions) image.Image {
	bounds := img.Bounds()

	base := image.NewRGBA(bounds)
	draw.Draw(base, bounds, img, bounds.Min, draw.Src)

	mark := renderWatermarkText(opts.Text, opts.Opacity)
	mb := mark.Bounds()

	// Если водяной знак крупнее самого фото - на очень маленьких изображениях
	// просто выходим без изменений, чтобы не сломать картинку обрезкой знака.
	if mb.Dx() > bounds.Dx() || mb.Dy() > bounds.Dy() {
		return base
	}

	offset := watermarkOffset(opts.Position, bounds.Dx(), bounds.Dy(), mb.Dx(), mb.Dy())
	dstRect := image.Rect(0, 0, mb.Dx(), mb.Dy()).Add(offset).Add(bounds.Min)

	draw.Draw(base, dstRect, mark, mb.Min, draw.Over)
	return base
}

// watermarkOffset считает координаты левого верхнего угла водяного знака
// относительно левого верхнего угла фотографии.
func watermarkOffset(pos imginfo.WatermarkPosition, imgW, imgH, markW, markH int) image.Point {
	switch pos {
	case imginfo.WatermarkTopLeft:
		return image.Pt(watermarkMargin, watermarkMargin)
	case imginfo.WatermarkTopRight:
		return image.Pt(imgW-markW-watermarkMargin, watermarkMargin)
	case imginfo.WatermarkBottomLeft:
		return image.Pt(watermarkMargin, imgH-markH-watermarkMargin)
	case imginfo.WatermarkBottomRight:
		return image.Pt(imgW-markW-watermarkMargin, imgH-markH-watermarkMargin)
	case imginfo.WatermarkCenter:
		return image.Pt((imgW-markW)/2, (imgH-markH)/2)
	default:
		return image.Pt(watermarkMargin, watermarkMargin)
	}
}
