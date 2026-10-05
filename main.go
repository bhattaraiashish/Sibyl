package main

import (
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Config struct {
	NotificationInterval time.Duration
	Token                string
}

type ConfigFile struct {
	NotificationInterval string `json:"notification_interval"`
	Token                string `json:"token"`
}

func LoadFromConfigFile(path *string) Config {
	config := Config{
		NotificationInterval: 30 * time.Minute,
	}

	if path == nil {
		return config
	}

	slog.Info("loading config file", slog.String("path", *path))

	data, err := os.ReadFile(*path)
	if err != nil {
		return config
	}

	var file ConfigFile

	if err := json.Unmarshal(data, &file); err != nil {
		return config
	}

	config.Token = file.Token

	if file.NotificationInterval != "" {
		interval, err := time.ParseDuration(file.NotificationInterval)
		if err == nil && interval > 0 {
			config.NotificationInterval = interval
		}
	}

	return config
}

func main() {
	debug := flag.Bool("debug", false, "enable debug mode")
	configPath := flag.String("config", "", "path to config.json file")
	flag.Parse()

	config := LoadFromConfigFile(configPath)
	SetAnimeCheckInterval(config.NotificationInterval)
	SetRSSCheckInterval(config.NotificationInterval)

	ctx := &BotContext{}
	ctx.IsDebug = *debug

	//----------
	// Register commands

	InitCommonFeatures(ctx)
	InitAnimeFeatures(ctx)
	InitRSSFeatures(ctx)

	//----------
	// Start the bot

	//----------
	// Dynamic Presence
	ctx.RegisterTimer(
		5*time.Minute,
		UpdatePresence,
	)
	ctx.RegisterFirstMessage(UpdatePresence)

	token := strings.TrimSpace(config.Token)
	if token == "" {
		panic("empty token in config")
	}

	err := ctx.Login(token)
	if err != nil {
		slog.Error("failed to login", slog.Any("err", err))
		return
	}
}
