package imginfo

import (
	"strings"

	apperrors "github.com/QtaroAXE/image-redactor/internal/domain/errors"
)

// WatermarkPosition - угол/место, куда наносится водяной знак.
type WatermarkPosition string

const (
	WatermarkTopLeft     WatermarkPosition = "top-left"
	WatermarkTopRight    WatermarkPosition = "top-right"
	WatermarkBottomLeft  WatermarkPosition = "bottom-left"
	WatermarkBottomRight WatermarkPosition = "bottom-right"
	WatermarkCenter      WatermarkPosition = "center"
)

// WatermarkOptions - параметры текстового водяного знака.
type WatermarkOptions struct {
	Text     string            // текст водяного знака
	Position WatermarkPosition // расположение на фото
	Opacity  float64           // прозрачность, от 0 (невидимо) до 1 (непрозрачно)
}

// NewWatermarkOptions создаёт параметры водяного знака с проверкой значений.
func NewWatermarkOptions(text string, position WatermarkPosition, opacity float64) (WatermarkOptions, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return WatermarkOptions{}, apperrors.New(apperrors.TypeInvalidInput, "текст водяного знака не может быть пустым")
	}
	switch position {
	case WatermarkTopLeft, WatermarkTopRight, WatermarkBottomLeft, WatermarkBottomRight, WatermarkCenter:
	default:
		return WatermarkOptions{}, apperrors.New(apperrors.TypeInvalidInput, "неизвестное расположение водяного знака").WithContext("position", string(position))
	}
	if opacity <= 0 || opacity > 1 {
		return WatermarkOptions{}, apperrors.New(apperrors.TypeInvalidInput, "прозрачность водяного знака должна быть в диапазоне от 0 (не включительно) до 1").WithContext("opacity", opacity)
	}
	return WatermarkOptions{Text: text, Position: position, Opacity: opacity}, nil
}
