// Package fs - работа с файловой системой: поиск входных файлов, сохранение
// результатов, перенос файлов в processed/errors после обработки.
package fs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	apperrors "github.com/QtaroAXE/image-redactor/internal/domain/errors"
)

// FileSystem - обёртка над файловыми операциями приложения.
// mu защищает операции переноса/записи файлов от гонок при параллельной
// обработке несколькими воркерами.
type FileSystem struct {
	inputDir     string
	outputDir    string
	processedDir string
	errorDir     string
	mu           sync.RWMutex
}

// NewFileSystem создаёт файловую систему и гарантирует существование всех
// нужных директорий (создаёт их при необходимости).
func NewFileSystem(inputDir, outputDir, processedDir, errorDir string) (*FileSystem, error) {
	dirs := []string{inputDir, outputDir, processedDir, errorDir}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, apperrors.WrapWithFile(
				err,
				apperrors.TypeInternal,
				fmt.Sprintf("не удалось создать директорию: %s", dir),
			).WithContext("directory", dir)
		}
	}

	return &FileSystem{
		inputDir:     inputDir,
		outputDir:    outputDir,
		processedDir: processedDir,
		errorDir:     errorDir,
	}, nil
}

// GetInputFiles возвращает список путей ко всем изображениям во входной директории.
func (fsys *FileSystem) GetInputFiles() ([]string, error) {
	fsys.mu.RLock()
	defer fsys.mu.RUnlock()

	var files []string

	err := filepath.Walk(fsys.inputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return apperrors.WrapWithFile(
				err,
				apperrors.TypeInternal,
				"не удалось обойти директорию",
			).WithContext("path", path)
		}
		if info.IsDir() {
			return nil
		}
		if fsys.isImageFile(path) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// isImageFile определяет, похож ли файл на изображение, по расширению.
func (fsys *FileSystem) isImageFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp":
		return true
	default:
		return false
	}
}

// GetOutputPath строит путь для результата на основе исходного имени файла
// и целевого расширения (jpg/png/webp).
func (fsys *FileSystem) GetOutputPath(inputPath string, extension string) string {
	fileName := filepath.Base(inputPath)
	ext := filepath.Ext(fileName)
	nameWithoutExt := fileName[:len(fileName)-len(ext)]

	return filepath.Join(fsys.outputDir, nameWithoutExt+"."+extension)
}

// MoveToProcessed переносит успешно обработанный исходный файл в директорию processed.
func (fsys *FileSystem) MoveToProcessed(path string) error {
	fsys.mu.Lock()
	defer fsys.mu.Unlock()

	dest := filepath.Join(fsys.processedDir, filepath.Base(path))
	if err := moveFile(path, dest); err != nil {
		return apperrors.WrapWithFile(
			err,
			apperrors.TypeIO,
			"не удалось перенести файл в директорию processed",
		).WithContext("source", path).WithContext("destination", dest)
	}
	return nil
}

// MoveToError переносит файл, который не удалось обработать, в директорию errors.
func (fsys *FileSystem) MoveToError(path string) error {
	fsys.mu.Lock()
	defer fsys.mu.Unlock()

	dest := filepath.Join(fsys.errorDir, filepath.Base(path))
	if err := moveFile(path, dest); err != nil {
		return apperrors.WrapWithFile(
			err,
			apperrors.TypeIO,
			"не удалось перенести файл в директорию errors",
		).WithContext("source", path).WithContext("destination", dest)
	}
	return nil
}

// moveFile переносит файл. Сначала пробует быстрый os.Rename, а если это не
// получилось (например, source и destination находятся на разных дисках/томах -
// частый случай в Docker-контейнерах с смонтированными volume) - копирует
// содержимое и удаляет исходный файл.
func moveFile(src, dest string) error {
	if err := os.Rename(src, dest); err == nil {
		return nil
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	in.Close()

	return os.Remove(src)
}

// ReadFile читает содержимое файла целиком.
func (fsys *FileSystem) ReadFile(path string) ([]byte, error) {
	fsys.mu.RLock()
	defer fsys.mu.RUnlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, apperrors.WrapWithFile(
			err,
			apperrors.TypeIO,
			"не удалось прочитать файл",
		).WithContext("path", path)
	}
	return data, nil
}

// FileExists проверяет, существует ли файл по указанному пути.
func (fsys *FileSystem) FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// GetFileSize возвращает размер файла в байтах.
func (fsys *FileSystem) GetFileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, apperrors.WrapWithFile(
			err,
			apperrors.TypeNotFound,
			"не удалось получить информацию о файле",
		).WithContext("path", path)
	}
	return info.Size(), nil
}

// CleanProcessed удаляет файлы из директории processed.
// Если maxAge <= 0 - удаляются ВСЕ файлы в директории (полная очистка).
// Если maxAge > 0 - удаляются только файлы, которые не изменялись дольше maxAge
// (по времени модификации файла, mtime).
// Возвращает количество удалённых файлов и первую встреченную ошибку, если она была -
// при этом функция не останавливается на первой ошибке, а пытается удалить
// остальные файлы (иначе один "залоченный" файл помешал бы почистить всё остальное).
func (fsys *FileSystem) CleanProcessed(maxAge time.Duration) (int, error) {
	fsys.mu.Lock()
	defer fsys.mu.Unlock()

	entries, err := os.ReadDir(fsys.processedDir)
	if err != nil {
		return 0, apperrors.WrapWithFile(
			err,
			apperrors.TypeIO,
			"не удалось прочитать директорию processed",
		).WithContext("directory", fsys.processedDir)
	}

	removed := 0
	var firstErr error

	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		path := filepath.Join(fsys.processedDir, entry.Name())

		if maxAge > 0 {
			info, err := entry.Info()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			if now.Sub(info.ModTime()) < maxAge {
				continue // файл ещё не устарел - пропускаем
			}
		}

		if err := os.Remove(path); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		removed++
	}

	if firstErr != nil {
		return removed, apperrors.WrapWithFile(
			firstErr,
			apperrors.TypeIO,
			"не все файлы удалось удалить из директории processed",
		)
	}
	return removed, nil
}

func (fsys *FileSystem) GetInputDir() string  { return fsys.inputDir }
func (fsys *FileSystem) GetOutputDir() string { return fsys.outputDir }
