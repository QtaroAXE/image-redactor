// cmd/app/main.go - точка входа в приложение.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/QtaroAXE/image-redactor/internal/config"
	"github.com/QtaroAXE/image-redactor/internal/infra/ui"
)

func main() {
	configPath := flag.String("config", "configs/config.json", "путь к файлу конфигурации")
	useEnv := flag.Bool("env", false, "использовать переменные окружения вместо файла конфигурации")
	flag.Parse()

	var cfg *config.Config
	var err error

	if *useEnv {
		cfg = config.LoadFromEnv()
		fmt.Println("Конфигурация загружена из переменных окружения")
	} else {
		cfg, err = config.LoadConfig(*configPath)
		if err != nil {
			log.Fatalf("Не удалось загрузить конфигурацию: %v", err)
		}
		fmt.Printf("Конфигурация загружена из: %s (если файла не было - использованы значения по умолчанию)\n", *configPath)
	}

	if err := cfg.Validate(); err != nil {
		log.Fatalf("Некорректная конфигурация: %v", err)
	}

	fmt.Printf("Настройки: вход=%s, выход=%s, воркеров=%d, качество=%d\n",
		cfg.Input, cfg.Output, cfg.Workers, cfg.Quality)

	// Обрабатываем Ctrl+C / сигнал завершения. os.Exit(0) прерывает процесс
	// немедленно, без ожидания завершения текущей пакетной обработки -
	// если в этот момент шло сохранение файлов, часть из них может остаться
	// в промежуточном состоянии. Для полноценного graceful shutdown нужно
	// прокидывать общий context.Context вглубь ConsoleUI и WorkerPool -
	// это осознанно оставлено на будущее ради простоты текущей реализации.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nЗавершение работы...")
		os.Exit(0)
	}()

	consoleUI := ui.NewConsoleUI(cfg)
	if err := consoleUI.Run(); err != nil {
		log.Fatalf("Ошибка интерфейса: %v", err)
	}
}
