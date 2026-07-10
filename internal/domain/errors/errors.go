// Package errors содержит собственный тип ошибки приложения (AppError),
// который добавляет к обычной ошибке тип, контекст и место возникновения.
// Названа "errors", а не "apperrors", исторически; во всех остальных файлах
// пакет импортируется под псевдонимом apperrors, чтобы не путать со
// стандартным пакетом "errors".
package errors

import (
	"fmt"
	"path/filepath"
	"runtime"
	"time"
)

// AppError - структурированная ошибка приложения.
type AppError struct {
	Time time.Time `json:"time"` // когда произошла ошибка

	Type string `json:"type"` // категория ошибки (см. константы Type* ниже)

	Message string `json:"message"` // человекочитаемое описание

	Err error `json:"-"` // исходная ошибка, если есть (для Unwrap)

	Context map[string]interface{} `json:"context,omitempty"` // дополнительные данные

	File     string `json:"file,omitempty"`     // файл, где создана ошибка
	Line     int    `json:"line,omitempty"`     // строка
	Function string `json:"function,omitempty"` // функция
}

// Категории ошибок. Используются, например, чтобы решить,
// стоит ли повторить операцию (см. isRetryableError в pipeline).
const (
	TypeInvalidInput  = "INVALID_INPUT"
	TypeNotFound      = "NOT_FOUND"
	TypeAlreadyExists = "ALREADY_EXISTS"
	TypeInternal      = "INTERNAL_ERROR"
	TypeUnsupported   = "UNSUPPORTED"
	TypePermission    = "PERMISSION_DENIED"
	TypeIO            = "IO_ERROR"
	TypeDecode        = "DECODE_ERROR"
	TypeEncode        = "ENCODE_ERROR"
	TypeCompress      = "COMPRESS_ERROR"
	TypeValidate      = "VALIDATE_ERROR"
	TypeUnknown       = "UNKNOWN_ERROR"
	TypeTimeout       = "TIME_OUT"
)

// New создаёт новую ошибку приложения без "родительской" ошибки.
func New(errType, message string) *AppError {
	err := &AppError{
		Time:    time.Now(),
		Type:    errType,
		Message: message,
		Context: make(map[string]interface{}),
	}
	err.captureFileInfo(2)
	return err
}

// NewWithFile - то же самое, что New (оставлено для обратной совместимости вызовов).
func NewWithFile(errType, message string) *AppError {
	return New(errType, message)
}

// Wrap оборачивает существующую ошибку err, добавляя тип и сообщение.
// Если err == nil, возвращает nil (удобно использовать сразу после вызова функции).
func Wrap(err error, errType, message string) *AppError {
	if err == nil {
		return nil
	}
	appErr := &AppError{
		Time:    time.Now(),
		Type:    errType,
		Message: message,
		Err:     err,
		Context: make(map[string]interface{}),
	}
	appErr.captureFileInfo(2)
	return appErr
}

// WrapWithFile - то же самое, что Wrap (оставлено для обратной совместимости вызовов).
func WrapWithFile(err error, errType, message string) *AppError {
	return Wrap(err, errType, message)
}

// captureFileInfo запоминает файл, строку и функцию, в которой была создана ошибка.
// skip - сколько уровней стека пропустить, чтобы попасть в код вызывающей стороны,
// а не внутрь самого пакета errors.
func (e *AppError) captureFileInfo(skip int) {
	pc, file, line, ok := runtime.Caller(skip)
	if ok {
		e.File = filepath.Base(file)
		e.Line = line

		fn := runtime.FuncForPC(pc)
		if fn != nil {
			e.Function = fn.Name()
		}
	}
}

// Error реализует интерфейс error. Формат: [ТИП] (файл:строка) сообщение: причина.
func (e *AppError) Error() string {
	location := ""
	if e.File != "" {
		location = fmt.Sprintf(" (%s:%d)", e.File, e.Line)
	}

	if e.Err != nil {
		return fmt.Sprintf("[%s]%s %s: %v", e.Type, location, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s]%s %s", e.Type, location, e.Message)
}

// Unwrap позволяет использовать errors.Is / errors.As из стандартной библиотеки.
func (e *AppError) Unwrap() error {
	return e.Err
}

// WithContext добавляет произвольную пару ключ-значение в контекст ошибки.
func (e *AppError) WithContext(key string, value interface{}) *AppError {
	e.Context[key] = value
	return e
}

// WithPath добавляет путь к файлу, с которым связана ошибка.
func (e *AppError) WithPath(path string) *AppError {
	e.Context["path"] = path
	return e
}

// WithWorker добавляет id воркера, в котором произошла ошибка.
func (e *AppError) WithWorker(id int) *AppError {
	e.Context["worker_id"] = id
	return e
}
