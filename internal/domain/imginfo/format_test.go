package imginfo_test

import (
	"testing"

	"github.com/QtaroAXE/image-redactor/internal/domain/imginfo"
)

// TestNewFormat_Valid проверяет разбор корректных строк формата.
func TestNewFormat_Valid(t *testing.T) {
	cases := []struct {
		in   string
		want imginfo.Format
	}{
		{"jpeg", imginfo.FormatJPEG},
		{"JPEG", imginfo.FormatJPEG},
		{"png", imginfo.FormatPNG},
		{"  png", imginfo.FormatPNG},
		{"png  ", imginfo.FormatPNG},
		{"PNG", imginfo.FormatPNG},
		{"jpg", imginfo.FormatJPEG},
		{"JPG", imginfo.FormatJPEG},
		{"webp", imginfo.FormatWebP},
		{"WEBP", imginfo.FormatWebP},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := imginfo.NewFormat(tc.in)
			if err != nil {
				t.Fatalf("NewFormat(%q) вернул ошибку: %v", tc.in, err)
			}
			if !got.Equals(tc.want.String()) {
				t.Errorf("NewFormat(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestNewFormat_Invalid проверяет, что некорректные строки дают ошибку.
func TestNewFormat_Invalid(t *testing.T) {
	cases := []string{"", " ", "bmp", "gif", "tiff", "something"}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			got, err := imginfo.NewFormat(in)
			if err == nil {
				t.Fatalf("NewFormat(%q) = %v, ожидалась ошибка", in, got)
			}
			if !got.IsZero() {
				t.Errorf("NewFormat(%q) вернул не пустой формат при ошибке: %v", in, got)
			}
		})
	}
}

// TestFormat_StringAndIsZero проверяет базовые геттеры формата.
func TestFormat_StringAndIsZero(t *testing.T) {
	if imginfo.FormatJPEG.String() != "jpeg" {
		t.Errorf("FormatJPEG.String() = %q, want %q", imginfo.FormatJPEG.String(), "jpeg")
	}
	if imginfo.FormatJPEG.Extension() != "jpg" {
		t.Errorf("FormatJPEG.Extension() = %q, want %q", imginfo.FormatJPEG.Extension(), "jpg")
	}
	var zero imginfo.Format
	if !zero.IsZero() {
		t.Errorf("пустой Format должен возвращать IsZero() == true")
	}
	if imginfo.FormatPNG.IsZero() {
		t.Errorf("FormatPNG не должен быть пустым")
	}
}
