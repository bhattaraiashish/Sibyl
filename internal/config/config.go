package config

import "time"

type SibylConfig struct {
	NotificationInterval time.Duration
}

type SibylConfigFile struct {
	NotificationInterval string `json:"notification_interval"`
}
