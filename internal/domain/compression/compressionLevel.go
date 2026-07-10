package compression

import (
	"fmt"

	apperrors "github.com/QtaroAXE/image-redactor/internal/domain/errors"
)

// CompressionLevel - уровень сжатия PNG от 1 (быстро, слабое сжатие)
// до 10 (медленно, сильное сжатие).
type CompressionLevel struct {
	value int
}

// DefaultCompressionLevel - уровень сжатия по умолчанию.
var DefaultCompressionLevel = CompressionLevel{6}

func (g CompressionLevel) Value() int {
	return g.value
}
func (g CompressionLevel) IsZero() bool {
	return g.value == 0
}

// NewCompressionLevel создаёт уровень сжатия с проверкой диапазона 1..10.
// Если передан 0 - возвращается уровень по умолчанию.
func NewCompressionLevel(input int) (CompressionLevel, error) {
	if input == 0 {
		return DefaultCompressionLevel, nil
	}
	if input < 1 || input > 10 {
		err := fmt.Errorf("уровень сжатия должен быть от 1 до 10, получено %d", input)
		return CompressionLevel{}, apperrors.Wrap(
			err,
			apperrors.TypeInvalidInput,
			"некорректный уровень сжатия",
		).WithContext("level", input)
	}
	return CompressionLevel{value: input}, nil
}
