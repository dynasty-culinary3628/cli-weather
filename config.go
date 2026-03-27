package main

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds user-configurable settings stored in config.toml.
type Config struct {
	ZipCode        string `toml:"zip_code"`
	Units          string `toml:"units"`           // "imperial" or "metric"
	RefreshMinutes int    `toml:"refresh_minutes"` // how often to poll
	UserAgent      string `toml:"user_agent"`
}

// loadConfig finds, reads, and returns the configuration.
// It creates a default config file if none exists.
// Returns the config, the path to the config file, and any error.
func loadConfig() (Config, string, error) {
	cfg := Config{
		Units:          "imperial",
		RefreshMinutes: 10,
		UserAgent:      "cli-weather/1.0 (github.com/user/cli-weather)",
	}

	path, err := configFilePath()
	if err != nil {
		return cfg, "", err
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, []byte(defaultConfigTOML), 0o644); err != nil {
			return cfg, path, err
		}
		return cfg, path, nil
	}

	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return cfg, path, err
	}

	if cfg.Units != "imperial" && cfg.Units != "metric" {
		cfg.Units = "imperial"
	}
	if cfg.RefreshMinutes < 1 {
		cfg.RefreshMinutes = 10
	}

	return cfg, path, nil
}

// configFilePath returns the config file path, preferring a config.toml in the
// same directory as the binary over the OS config directory.
func configFilePath() (string, error) {
	if exe, err := os.Executable(); err == nil {
		local := filepath.Join(filepath.Dir(exe), "config.toml")
		if _, err := os.Stat(local); err == nil {
			return local, nil
		}
	}

	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	appDir := filepath.Join(dir, "cli-weather")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(appDir, "config.toml"), nil
}

const defaultConfigTOML = `# cli-weather configuration
# Edit this file to customize your weather display.

# Your US ZIP code (required)
zip_code = ""

# Units: "imperial" (°F, mph, inHg, mi) or "metric" (°C, km/h, hPa, km)
units = "imperial"

# How often to refresh weather data, in minutes.
# The weather.gov API updates observations hourly; 10 minutes is reasonable.
refresh_minutes = 10

# Identifies your app to the weather.gov API (required by their terms of service).
user_agent = "cli-weather/1.0 (github.com/user/cli-weather)"
`
