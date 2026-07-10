package imginfo

import (
	"strings"
	"testing"
)

// containsAll проверяет, что сообщение об ошибке содержит все ожидаемые подстроки.
// Точное совпадение строк не используется намеренно: AppError.Error() включает
// имя файла и номер строки, которые могут меняться при рефакторинге.
func containsAll(t *testing.T, got string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(got, p) {
			t.Errorf("сообщение об ошибке %q не содержит ожидаемую часть %q", got, p)
		}
	}
}

func TestNewSourceImage(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		format    Format
		sizeBytes int64
		wantErr   bool
		errPart   string
	}{
		{
			name:      "корректное изображение",
			path:      "/path/to/image.jpg",
			format:    FormatJPEG,
			sizeBytes: 1024,
			wantErr:   false,
		},
		{
			name:      "корректное изображение с пробелами в пути",
			path:      "  /path/to/image.jpg  ",
			format:    FormatPNG,
			sizeBytes: 2048,
			wantErr:   false,
		},
		{
			name:      "пустой путь",
			path:      "",
			format:    FormatJPEG,
			sizeBytes: 1024,
			wantErr:   true,
			errPart:   "путь к файлу не может быть пустым",
		},
		{
			name:      "путь из одних пробелов",
			path:      "   ",
			format:    FormatJPEG,
			sizeBytes: 1024,
			wantErr:   true,
			errPart:   "путь к файлу не может быть пустым",
		},
		{
			name:      "пустой формат",
			path:      "/path/to/image.jpg",
			format:    Format{},
			sizeBytes: 1024,
			wantErr:   true,
			errPart:   "формат изображения не указан",
		},
		{
			name:      "отрицательный размер",
			path:      "/path/to/image.jpg",
			format:    FormatJPEG,
			sizeBytes: -100,
			wantErr:   true,
			errPart:   "размер файла не может быть отрицательным",
		},
		{
			name:      "нулевой размер - это нормально",
			path:      "/path/to/image.jpg",
			format:    FormatPNG,
			sizeBytes: 0,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewSourceImage(tt.path, tt.format, tt.sizeBytes)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("NewSourceImage() ожидалась ошибка, но её нет")
				}
				containsAll(t, err.Error(), tt.errPart)
				return
			}

			if err != nil {
				t.Fatalf("NewSourceImage() неожиданная ошибка: %v", err)
			}

			expectedPath := strings.TrimSpace(tt.path)
			if got.Path() != expectedPath {
				t.Errorf("Path() = %v, want %v", got.Path(), expectedPath)
			}
			if got.Format() != tt.format {
				t.Errorf("Format() = %v, want %v", got.Format(), tt.format)
			}
			if got.SizeBytes() != tt.sizeBytes {
				t.Errorf("SizeBytes() = %v, want %v", got.SizeBytes(), tt.sizeBytes)
			}
			if got.HasDimensions() {
				t.Error("HasDimensions() должен быть false для нового изображения")
			}
		})
	}
}

func TestSourceImage_WithDimensions(t *testing.T) {
	baseImage, err := NewSourceImage("/test/image.jpg", FormatJPEG, 1024)
	if err != nil {
		t.Fatalf("не удалось создать базовое изображение: %v", err)
	}

	tests := []struct {
		name    string
		width   int
		height  int
		wantErr bool
	}{
		{"корректные размеры", 1920, 1080, false},
		{"квадратные размеры", 800, 800, false},
		{"нулевая ширина", 0, 1080, true},
		{"нулевая высота", 1920, 0, true},
		{"отрицательная ширина", -100, 1080, true},
		{"отрицательная высота", 1920, -100, true},
		{"оба отрицательные", -1920, -1080, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			img, err := baseImage.WithDimensions(tt.width, tt.height)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("WithDimensions() ожидалась ошибка, но её нет")
				}
				containsAll(t, err.Error(), "ширина и высота должны быть положительными")
				return
			}

			if err != nil {
				t.Fatalf("WithDimensions() неожиданная ошибка: %v", err)
			}

			if baseImage.Width() != 0 || baseImage.Height() != 0 {
				t.Error("исходное изображение не должно было измениться (иммутабельность)")
			}
			if img.Width() != tt.width || img.Height() != tt.height {
				t.Errorf("Width/Height = %vx%v, want %vx%v", img.Width(), img.Height(), tt.width, tt.height)
			}
			if !img.HasDimensions() {
				t.Error("HasDimensions() должен быть true, когда размеры заданы")
			}
		})
	}
}

func TestSourceImage_Immutability(t *testing.T) {
	original, err := NewSourceImage("/test.jpg", FormatJPEG, 1000)
	if err != nil {
		t.Fatalf("не удалось создать изображение: %v", err)
	}

	modified, err := original.WithDimensions(1920, 1080)
	if err != nil {
		t.Fatalf("не удалось задать размеры: %v", err)
	}

	if original.Width() != 0 || original.Height() != 0 {
		t.Error("оригинал не должен иметь размеров")
	}
	if modified.Width() != 1920 || modified.Height() != 1080 {
		t.Error("копия должна иметь заданные размеры")
	}
	if original.Path() != modified.Path() || original.Format() != modified.Format() || original.SizeBytes() != modified.SizeBytes() {
		t.Error("остальные поля должны совпадать")
	}
}
