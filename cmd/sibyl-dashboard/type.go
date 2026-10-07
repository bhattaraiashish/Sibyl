package main

import (
	"github.com/bhattaraiashish/Sibyl/internal/config"
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
	ID          string
	DisplayName string
	Username    string
	AvatarURL   string
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

type SettingsData struct {
	LayoutData
	Config config.SibylConfig
}

type ErrorData struct {
	Title    string
	Message  string
	ReturnTo string
}
