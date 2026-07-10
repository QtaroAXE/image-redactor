package imginfo

import (
	apperrors "github.com/QtaroAXE/image-redactor/internal/domain/errors"
)

// CropRect - прямоугольник обрезки в пикселях, отсчитывается от верхнего левого угла.
type CropRect struct {
	X, Y          int
	Width, Height int
}

// NewCropRect создаёт прямоугольник обрезки с проверкой корректности значений.
// Проверка "помещается ли прямоугольник в конкретное изображение" делается
// отдельно, в момент применения (см. ClampTo), потому что размер изображения
// заранее не всегда известен (например, при пакетной обработке разных файлов).
func NewCropRect(x, y, width, height int) (CropRect, error) {
	if width <= 0 || height <= 0 {
		return CropRect{}, apperrors.New(
			apperrors.TypeInvalidInput,
			"ширина и высота обрезки должны быть положительными",
		).WithContext("width", width).WithContext("height", height)
	}
	if x < 0 || y < 0 {
		return CropRect{}, apperrors.New(
			apperrors.TypeInvalidInput,
			"координаты обрезки не могут быть отрицательными",
		).WithContext("x", x).WithContext("y", y)
	}
	return CropRect{X: x, Y: y, Width: width, Height: height}, nil
}

// ClampTo подгоняет прямоугольник под реальные размеры изображения:
// если он выходит за границы - обрезается по границе, а не приводит к ошибке.
// Это важно при пакетной обработке файлов разного размера с одними и теми же
// координатами обрезки.
func (c CropRect) ClampTo(imgWidth, imgHeight int) CropRect {
	x, y := c.X, c.Y
	if x > imgWidth {
		x = imgWidth
	}
	if y > imgHeight {
		y = imgHeight
	}
	w, h := c.Width, c.Height
	if x+w > imgWidth {
		w = imgWidth - x
	}
	if y+h > imgHeight {
		h = imgHeight - y
	}
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	return CropRect{X: x, Y: y, Width: w, Height: h}
}

// CropPreset - заранее заготовленный формат обрезки по соотношению сторон.
// Реальный прямоугольник для конкретного изображения считается по центру,
// без увеличения (изображение никогда не растягивается, только обрезается).
type CropPreset string

const (
	CropPresetSquare    CropPreset = "square"    // 1:1, квадрат (например, для аватарки)
	CropPresetPortrait  CropPreset = "portrait"  // 4:5, вертикальный пост
	CropPresetStory     CropPreset = "story"     // 9:16, история/сторис
	CropPresetWidescreen CropPreset = "widescreen" // 16:9, широкоформатное фото/видео-превью
)

// PresetRatios - соотношения сторон (ширина/высота) для каждого пресета,
// используются и в коде, и для отображения пользователю в консоли.
var presetRatios = map[CropPreset][2]int{
	CropPresetSquare:     {1, 1},
	CropPresetPortrait:   {4, 5},
	CropPresetStory:      {9, 16},
	CropPresetWidescreen: {16, 9},
}

// IsValid проверяет, что пресет из числа известных.
func (p CropPreset) IsValid() bool {
	_, ok := presetRatios[p]
	return ok
}

// RectForImage считает прямоугольник обрезки по центру изображения
// для заданного соотношения сторон пресета.
func (p CropPreset) RectForImage(imgWidth, imgHeight int) (CropRect, error) {
	ratio, ok := presetRatios[p]
	if !ok {
		return CropRect{}, apperrors.New(apperrors.TypeInvalidInput, "неизвестный пресет обрезки").WithContext("preset", string(p))
	}
	targetW, targetH := ratio[0], ratio[1]

	// Подбираем максимальный прямоугольник нужных пропорций, который
	// помещается в исходное изображение, и центрируем его.
	w := imgWidth
	h := w * targetH / targetW
	if h > imgHeight {
		h = imgHeight
		w = h * targetW / targetH
	}
	x := (imgWidth - w) / 2
	y := (imgHeight - h) / 2
	return CropRect{X: x, Y: y, Width: w, Height: h}, nil
}
