// Package imginfo содержит "модели" изображений: формат, исходное изображение,
// целевое изображение, параметры обрезки и водяного знака.
package imginfo

import (
	"strings"

	apperrors "github.com/QtaroAXE/image-redactor/internal/domain/errors"
)

// Format - формат изображения (jpeg/png/webp). Реализован как value object,
// чтобы нельзя было создать "неправильный" формат в обход конструктора NewFormat.
type Format struct {
	name string
}

// Поддерживаемые форматы.
var (
	FormatJPEG = Format{name: "jpeg"}
	FormatPNG  = Format{name: "png"}
	FormatWebP = Format{name: "webp"}
)

// String возвращает имя формата (jpeg/png/webp).
func (f Format) String() string {
	return f.name
}

// IsZero сообщает, что формат не был установлен (пустое значение по умолчанию).
func (f Format) IsZero() bool {
	return f.name == ""
}

// Equals сравнивает формат со строкой (например, "png").
func (f Format) Equals(otherF string) bool {
	return f.name == otherF
}

// Extension возвращает расширение файла для формата (без точки).
// Для jpeg возвращает "jpg", так как это более привычное расширение файлов.
func (f Format) Extension() string {
	if f.name == "jpeg" {
		return "jpg"
	}
	return f.name
}

// NewFormat разбирает строку и возвращает соответствующий формат.
// Понимает варианты в любом регистре и с пробелами по краям: "JPG", " png " и т.д.
func NewFormat(input string) (Format, error) {
	inputForm := strings.TrimSpace(input)
	inputForm = strings.ToLower(inputForm)

	if inputForm == "" {
		return Format{}, apperrors.New(apperrors.TypeInvalidInput, "формат изображения не может быть пустым")
	}
	switch inputForm {
	case "jpeg", "jpg":
		return FormatJPEG, nil
	case "png":
		return FormatPNG, nil
	case "webp":
		return FormatWebP, nil
	default:
		return Format{}, apperrors.New(apperrors.TypeUnsupported, "неподдерживаемый формат изображения: "+inputForm).WithContext("format", inputForm)
	}
}
