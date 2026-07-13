package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
)

// Config - настройки приложения.
type Config struct {
	Input     string `json:"input"`
	Output    string `json:"output"`
	Processed string `json:"processed"`
	Errors    string `json:"errors"`
	Workers   int    `json:"workers"`
	Quality   int    `json:"quality"`

	// ProcessedTTLHours - через сколько часов файлы в директории processed
	// считаются "старыми" и удаляются автоматически при выходе из приложения.
	// 0 (по умолчанию) - автоочистка отключена, файлы хранятся бессрочно,
	// пока пользователь не удалит их вручную через меню.
	ProcessedTTLHours int `json:"processed_ttl_hours"`
}

// LoadConfig загружает конфиг из JSON-файла. Если файл не найден - это НЕ
// считается ошибкой: возвращаются значения по умолчанию (удобно для первого
// запуска, когда пользователь ещё не создавал config.json). А вот если файл
// есть, но повреждён (некорректный JSON) - это уже настоящая ошибка.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		cfg := &Config{}
		cfg.setDefaults()
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("не удалось разобрать JSON конфига: %w", err)
	}

	cfg.setDefaults()
	return &cfg, nil
}

// LoadFromEnv загружает конфиг из переменных окружения.
func LoadFromEnv() *Config {
	cfg := &Config{}

	if val := os.Getenv("INPUT_DIR"); val != "" {
		cfg.Input = val
	}
	if val := os.Getenv("OUTPUT_DIR"); val != "" {
		cfg.Output = val
	}
	if val := os.Getenv("PROCESSED_DIR"); val != "" {
		cfg.Processed = val
	}
	if val := os.Getenv("ERRORS_DIR"); val != "" {
		cfg.Errors = val
	}
	if val := os.Getenv("WORKERS"); val != "" {
		if workers, err := strconv.Atoi(val); err == nil {
			cfg.Workers = workers
		}
	}
	if val := os.Getenv("QUALITY"); val != "" {
		if quality, err := strconv.Atoi(val); err == nil {
			cfg.Quality = quality
		}
	}
	if val := os.Getenv("PROCESSED_TTL_HOURS"); val != "" {
		if ttl, err := strconv.Atoi(val); err == nil {
			cfg.ProcessedTTLHours = ttl
		}
	}

	cfg.setDefaults()
	return cfg
}

// setDefaults заполняет пустые поля значениями по умолчанию.
func (c *Config) setDefaults() {
	if c.Input == "" {
		c.Input = "./input"
	}
	if c.Output == "" {
		c.Output = "./output"
	}
	if c.Processed == "" {
		c.Processed = "./processed"
	}
	if c.Errors == "" {
		c.Errors = "./errors"
	}
	if c.Workers == 0 {
		c.Workers = 4
	}
	if c.Quality == 0 {
		c.Quality = 85
	}
}

// SaveConfig сохраняет конфиг в JSON-файл.
func SaveConfig(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Validate проверяет конфиг на корректность.
func (c *Config) Validate() error {
	if c.Workers < 1 {
		return fmt.Errorf("количество воркеров должно быть не меньше 1, получено %d", c.Workers)
	}
	if c.Quality < 1 || c.Quality > 100 {
		return fmt.Errorf("качество должно быть от 1 до 100, получено %d", c.Quality)
	}
	if c.ProcessedTTLHours < 0 {
		return fmt.Errorf("processed_ttl_hours не может быть отрицательным, получено %d", c.ProcessedTTLHours)
	}
	return nil
}
