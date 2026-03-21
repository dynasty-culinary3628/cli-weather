package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	var zip string
	flag.StringVar(&zip, "zip", "", "ZIP code for weather (overrides config file)")
	flag.Parse()

	cfg, cfgPath, err := loadConfig()
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	if zip != "" {
		cfg.ZipCode = zip
	}

	if cfg.ZipCode == "" {
		fmt.Fprintf(os.Stderr, "No ZIP code configured.\nEdit %s to set zip_code, or use: cli-weather -zip 10001\n", cfgPath)
		os.Exit(1)
	}

	p := tea.NewProgram(newModel(cfg), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatalf("Fatal error: %v", err)
	}
}
