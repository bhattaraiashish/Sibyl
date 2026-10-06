package main

import (
	"encoding/json"
	"os"

	"github.com/bhattaraiashish/Sibyl/internal/config"
)

func LoadOrCreateDefaultConfig() config.SibylConfigFile {
	data, err := os.ReadFile("config.json")
	if err != nil {
		return createDefaultConfig()
	}

	var cfg config.SibylConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return createDefaultConfig()
	}

	return cfg
}

func createDefaultConfig() config.SibylConfigFile {
	cfg := config.SibylConfigFile{
		NotificationInterval: "1h",
	}

	data, err := json.MarshalIndent(cfg, "", "    ")
	if err == nil {
		_ = os.WriteFile("config.json", data, 0644)
	}

	return cfg
}
