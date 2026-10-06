package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/bhattaraiashish/Sibyl/internal/config"
)

var (
	_DashboardMu   sync.RWMutex
	_DashboardData DashboardData
)

func GetDashboardData() DashboardData {
	_DashboardMu.RLock()
	defer _DashboardMu.RUnlock()

	return _DashboardData
}

func StartDashboardUpdater() {
	go func() {
		updateDashboardData()

		ticker := time.NewTicker(config.BotHeartbeatInterval)
		defer ticker.Stop()

		for range ticker.C {
			updateDashboardData()
		}
	}()
}

func updateDashboardData() {
	status := GetBotStatus()
	counts := GetDatabaseCounts()

	data := DashboardData{
		BotName:       status.Username,
		BotAvatar:     fmt.Sprintf("https://cdn.discordapp.com/avatars/%s/%s.png", status.UserID, status.Avatar),
		Online:        time.Since(status.LastSeen) < config.BotOnlineTimeout,
		Latency:       status.Latency.Milliseconds(),
		ConnectedAt:   status.ConnectedAt.Unix(),
		TimezoneCount: counts.Timezones,
		GuildCount:    counts.Guilds,
		RSSFeeds:      counts.RSSFeeds,
		AnimeCount:    counts.Anime,
	}

	_DashboardMu.Lock()
	_DashboardData = data
	_DashboardMu.Unlock()
}
