package config

import (
	"os"
	"strings"
	"time"
)

type SibylConfig struct {
	NotificationInterval time.Duration
}

type SibylConfigFile struct {
	NotificationInterval string `json:"notification_interval"`
}

func LoadToken() string {
	data, err := os.ReadFile("token")
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}
