// cmd/app/main.go
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
	configPath := flag.String("config", "configs/config.json", "config file path")
	useEnv := flag.Bool("env", false, "use environment variables")
	headless := flag.Bool("headless", false, "run without UI")
	flag.Parse()

	var cfg *config.Config
	var err error

	if *useEnv {
		cfg = config.LoadFromEnv()
		fmt.Println("Loaded configuration from environment variables")
	} else {
		cfg, err = config.LoadConfig(*configPath)
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
		fmt.Printf("Loaded configuration from: %s\n", *configPath)
	}

	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid config: %v", err)
	}

	fmt.Printf("Config: input=%s, output=%s, workers=%d, quality=%d\n",
		cfg.Input, cfg.Output, cfg.Workers, cfg.Quality)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nExiting...")
		os.Exit(0)
	}()

	if *headless {
		fmt.Println("Headless mode not implemented")
	} else {
		consoleUI := ui.NewConsoleUI(cfg)
		if err := consoleUI.Run(); err != nil {
			log.Fatalf("UI error: %v", err)
		}
	}
}
