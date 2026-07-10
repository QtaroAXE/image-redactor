package imginfo

import (
	"fmt"
	"strings"

	apperrors "github.com/QtaroAXE/image-redactor/internal/domain/errors"
)

// SourceImage - описание исходного изображения: где лежит, в каком формате,
// какого размера. Ширина/высота необязательны (0 значит "неизвестно").
type SourceImage struct {
	path      string
	format    Format
	sizeBytes int64
	width     int // 0 - размер неизвестен
	height    int // 0 - размер неизвестен
}

// NewSourceImage создаёт исходное изображение с проверкой входных данных.
func NewSourceImage(path string, format Format, sizeBytes int64) (SourceImage, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return SourceImage{}, apperrors.New(apperrors.TypeInvalidInput, "путь к файлу не может быть пустым")
	}
	if format.IsZero() {
		return SourceImage{}, apperrors.New(apperrors.TypeInvalidInput, "формат изображения не указан")
	}
	if sizeBytes < 0 {
		return SourceImage{}, apperrors.New(apperrors.TypeInvalidInput, "размер файла не может быть отрицательным").WithContext("size_bytes", sizeBytes)
	}
	return SourceImage{
		path:      trimmed,
		format:    format,
		sizeBytes: sizeBytes,
	}, nil
}

// WithDimensions возвращает копию изображения с заданными шириной и высотой.
// Исходный объект не изменяется (иммутабельность).
func (s SourceImage) WithDimensions(width, height int) (SourceImage, error) {
	if width <= 0 || height <= 0 {
		return SourceImage{}, fmt.Errorf("изображение: ширина и высота должны быть положительными, получено %dx%d", width, height)
	}
	s.width = width
	s.height = height
	return s, nil
}

func (s SourceImage) Path() string        { return s.path }
func (s SourceImage) Format() Format      { return s.format }
func (s SourceImage) SizeBytes() int64    { return s.sizeBytes }
func (s SourceImage) Width() int          { return s.width }
func (s SourceImage) Height() int         { return s.height }
func (s SourceImage) HasDimensions() bool { return s.width > 0 && s.height > 0 }
