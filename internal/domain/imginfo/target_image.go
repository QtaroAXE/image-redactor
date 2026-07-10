package imginfo

import (
	"strings"

	"github.com/QtaroAXE/image-redactor/internal/domain/compression"
	apperrors "github.com/QtaroAXE/image-redactor/internal/domain/errors"
)

// TargetImage - описание того, во что нужно превратить исходное изображение:
// путь сохранения, формат и параметры сжатия.
type TargetImage struct {
	path             string
	format           Format
	quality          compression.Quality
	compressionLevel compression.CompressionLevel
}

// NewTargetImage создаёт целевое изображение с проверкой пути и формата.
func NewTargetImage(path string, qual compression.Quality, compLevel compression.CompressionLevel, form Format) (TargetImage, error) {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return TargetImage{}, apperrors.New(apperrors.TypeInvalidInput, "путь сохранения не может быть пустым")
	}
	if form.IsZero() {
		return TargetImage{}, apperrors.New(apperrors.TypeInvalidInput, "формат изображения не указан")
	}
	return TargetImage{path: trimmedPath, quality: qual, compressionLevel: compLevel, format: form}, nil
}
func (t TargetImage) Path() string                              { return t.path }
func (t TargetImage) Quality() compression.Quality               { return t.quality }
func (t TargetImage) CompressionLevel() compression.CompressionLevel { return t.compressionLevel }
func (t TargetImage) Format() Format                              { return t.format }
