package main

import (
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"time"

	_ "modernc.org/sqlite"

	"github.com/bhattaraiashish/Sibyl/internal/config"
)

func LoadFromConfigFile(path *string) config.SibylConfig {
	cfg := config.SibylConfig{
		NotificationInterval: 30 * time.Minute,
	}

	if path == nil {
		return cfg
	}

	slog.Info("loading config file", slog.String("path", *path))

	data, err := os.ReadFile(*path)
	if err != nil {
		return cfg
	}

	var file config.SibylConfigFile

	if err := json.Unmarshal(data, &file); err != nil {
		return cfg
	}

	if file.NotificationInterval != "" {
		interval, err := time.ParseDuration(file.NotificationInterval)
		if err == nil && interval > 0 {
			cfg.NotificationInterval = interval
		}
	}

	return cfg
}

func watchConfig(path string, current config.SibylConfig) {
	var lastModified time.Time

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for range ticker.C {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}

		if info.ModTime().Equal(lastModified) {
			continue
		}

		lastModified = info.ModTime()

		newConfig := LoadFromConfigFile(&path)

		if newConfig.NotificationInterval == current.NotificationInterval {
			continue
		}

		slog.Info(
			"config reloaded",
			slog.Duration(
				"notification_interval",
				newConfig.NotificationInterval,
			),
		)

		SetAnimeCheckInterval(newConfig.NotificationInterval)
		SetRSSCheckInterval(newConfig.NotificationInterval)

		current = newConfig
	}
}

func main() {
	debug := flag.Bool("debug", false, "enable debug mode")
	configPath := flag.String("config", "", "path to config.json file")
	flag.Parse()

	cfg := LoadFromConfigFile(configPath)

	SetAnimeCheckInterval(cfg.NotificationInterval)
	SetRSSCheckInterval(cfg.NotificationInterval)

	ctx := &BotContext{}
	ctx.IsDebug = *debug

	//----------
	// Register commands

	InitCommonFeatures(ctx)
	InitAnimeFeatures(ctx)
	InitRSSFeatures(ctx)

	//----------
	// Dynamic Presence

	ctx.RegisterTimer(
		5*time.Minute,
		UpdatePresence,
	)
	ctx.RegisterFirstMessage(UpdatePresence)

	//----------
	// Start the bot

	token := config.LoadToken()
	if token == "" {
		panic("empty token file")
	}

	if err := ctx.Login(token); err != nil {
		slog.Error("failed to login", slog.Any("err", err))
		return
	}

	if *configPath != "" {
		go watchConfig(*configPath, cfg)
	}
}
