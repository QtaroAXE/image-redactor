// Package compression содержит параметры сжатия: качество (для jpeg/webp)
// и уровень сжатия (для png).
package compression

import (
	"fmt"

	apperrors "github.com/QtaroAXE/image-redactor/internal/domain/errors"
)

// Quality - качество сжатия от 1 до 100 (как в jpeg/webp).
type Quality struct {
	value int
}

// DefaultQuality - качество по умолчанию, если пользователь не указал своё.
var DefaultQuality = Quality{85}

func (q Quality) Value() int {
	return q.value
}
func (q Quality) IsZero() bool {
	return q.value == 0
}

// NewQuality создаёt значение качества с проверкой диапазона 1..100.
// Если передан 0 - возвращается качество по умолчанию (удобно для необязательных полей).
func NewQuality(input int) (Quality, error) {
	if input == 0 {
		return DefaultQuality, nil
	}
	if input < 1 || input > 100 {
		err := fmt.Errorf("качество должно быть от 1 до 100, получено %d", input)
		return Quality{}, apperrors.Wrap(
			err,
			apperrors.TypeInvalidInput,
			"некорректное качество сжатия",
		).WithContext("quality_value", input)
	}
	return Quality{value: input}, nil
}
