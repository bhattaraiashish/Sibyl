package main

import (
	"time"

	"github.com/disgoorg/snowflake/v2"
)

type Page struct {
	ID    string
	Path  string
	Title string
}

type Guild struct {
	ID   string
	Name string
	Icon string
}

type UserData struct {
	ID          snowflake.ID
	DisplayName string
	Username    string
	AvatarURL   string
	ExpiresAt   time.Time
	IsDeveloper bool
}

type FlashMessage struct {
	Type        string
	Description string
}

type LayoutData struct {
	Title         string
	Page          string
	Pages         []Page
	SearchQuery   string
	Guilds        []Guild
	SelectedGuild Guild
	User          *UserData
}

type DashboardData struct {
	BotName       string
	BotAvatar     string
	Online        bool
	Latency       int64
	ConnectedAt   int64
	GuildCount    int
	TimezoneCount int
	RSSFeeds      int
	AnimeCount    int
}

type OverviewData struct {
	LayoutData
	Dashboard DashboardData
}

type GuildsData struct {
	LayoutData
}

type ConfigData struct {
	NotificationIntervalMins int
	CanEdit                  bool
}

type SettingsData struct {
	LayoutData
	Config ConfigData
}

type Log struct {
	Name string
	Path string
}

type LogsData struct {
	LayoutData
	Logs []Log
}

type ErrorData struct {
	Title    string
	Message  string
	ReturnTo string
}
