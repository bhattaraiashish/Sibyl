package config

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"
)

const (
	BotHeartbeatInterval = 10 * time.Second
	BotOnlineTimeout     = 20 * time.Second

	LatestDatabaseVersion = 3
)

type SibylConfig struct {
	NotificationInterval time.Duration
}

func DefaultConfig() SibylConfig {
	return SibylConfig{
		NotificationInterval: 15 * time.Minute,
	}
}

func LoadEnvFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}

		slog.Error("failed to read env.json", "error", err)
		return
	}

	var env map[string]string
	if err := json.Unmarshal(data, &env); err != nil {
		slog.Error("failed to parse env.json", "error", err)
		return
	}

	for key, value := range env {
		if err := os.Setenv(key, value); err != nil {
			slog.Error("failed to set environment variable", "key", key, "error", err)
		}
	}
}

func LoadDatabase(path string) *sql.DB {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}

	if _, err := db.Exec(`
		PRAGMA journal_mode = WAL;
		PRAGMA busy_timeout = 5000;
	`); err != nil {
		slog.Error("failed to configure database", "error", err)
		os.Exit(1)
	}

	if err := db.Ping(); err != nil {
		slog.Error("failed to ping database", "error", err)
		db.Close()
		os.Exit(1)
	}

	if err := initDatabase(db); err != nil {
		slog.Error("failed to init database", "error", err)
		db.Close()
		os.Exit(1)
	}

	return db
}

func LoadConfig(db *sql.DB) SibylConfig {
	cfg := DefaultConfig()

	var value string

	err := db.QueryRow(`
		SELECT value
		FROM config
		WHERE key = 'notification_interval'
	`).Scan(&value)

	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			slog.Error("failed to load config",
				slog.Any("error", err),
			)
		}
		return cfg
	}

	interval, err := time.ParseDuration(value)
	if err != nil || interval <= time.Minute {
		slog.Warn("invalid notification interval",
			slog.String("value", value),
		)
		interval = cfg.NotificationInterval
	}

	cfg.NotificationInterval = interval

	return cfg
}

func SaveConfig(db *sql.DB, config SibylConfig) bool {
	_, err := db.Exec(`
		INSERT INTO config (key, value)
		VALUES ('notification_interval', ?)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value;
	`,
		config.NotificationInterval.String(),
	)
	if err != nil {
		slog.Error("failed to save config",
			slog.Any("error", err),
		)
		return false
	}
	return true
}

func initDatabase(db *sql.DB) error {
	version := getDatabaseVersion(db)

	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS guilds (
			guild_id TEXT PRIMARY KEY,
			features INTEGER NOT NULL DEFAULT 4294967295
		);

		CREATE TABLE IF NOT EXISTS user_timezones (
			user_id TEXT PRIMARY KEY,
			timezone TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS tracked_anime (
			guild_id TEXT NOT NULL,
			anime_id INTEGER NOT NULL,
			channel_id TEXT NOT NULL,
			last_notified_episode INTEGER NOT NULL DEFAULT 0,
			last_updated_time INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (guild_id, anime_id)
		);

		CREATE TABLE IF NOT EXISTS guild_anime_notifications (
			guild_id TEXT PRIMARY KEY,
			last_checked INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE IF NOT EXISTS rss_feeds (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			guild_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			url TEXT NOT NULL,
			last_item_id TEXT,
			last_checked INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE IF NOT EXISTS bot_status (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			user_id INTEGER NOT NULL DEFAULT 0,
			username TEXT NOT NULL DEFAULT '',
			avatar TEXT NOT NULL DEFAULT '',
			connected_at INTEGER NOT NULL DEFAULT 0,
			last_seen INTEGER NOT NULL DEFAULT 0,
			latency INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE IF NOT EXISTS user_sessions (
			id TEXT PRIMARY KEY,
			access_token TEXT NOT NULL,
			refresh_token TEXT NOT NULL,
			expires_at INTEGER NOT NULL,
			created_at INTEGER NOT NULL
		);

		CREATE TABLE IF NOT EXISTS config (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);

		INSERT OR IGNORE INTO config (key, value)
		VALUES
			('notification_interval', '15m');
	`)

	if err != nil {
		cfg := DefaultConfig()
		_, err = db.Exec(`
			INSERT OR IGNORE INTO config (key, value)
				VALUES (?, ?)
			`, "notification_interval",
			cfg.NotificationInterval.String())
	}

	if version != LatestDatabaseVersion {
		setDatabaseVersion(db, LatestDatabaseVersion)
	}

	return err
}

func getDatabaseVersion(db *sql.DB) int {
	var version int

	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		slog.Error("failed to get database version", "error", err)
		db.Close()
		os.Exit(1)
	}

	return version
}

func setDatabaseVersion(db *sql.DB, version int) {
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		slog.Error("failed to set database version", "version", version, "error", err)
		db.Close()
		os.Exit(1)
	}
}
