package main

import (
	"database/sql"
	"log"
	"time"

	"github.com/bhattaraiashish/Sibyl/internal/config"
	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/snowflake/v2"
	_ "modernc.org/sqlite"
)

var (
	_Database *sql.DB
	_Client   *bot.Client
)

type BotStatus struct {
	UserID      snowflake.ID
	Username    string
	Avatar      string
	ConnectedAt time.Time
	LastSeen    time.Time
	Latency     time.Duration
}

type DatabaseCounts struct {
	Guilds    int
	Timezones int
	Anime     int
	RSSFeeds  int
}

func InitStore(token string) {
	var err error

	_Database = config.LoadDatabase()

	_Client, err = disgo.New(token)
	if err != nil {
		log.Fatal(err)
	}
}

func GetBotStatus() BotStatus {
	var (
		userID      int64
		username    string
		avatar      string
		connectedAt int64
		lastSeen    int64
		latency     int64
	)

	err := _Database.QueryRow(`
		SELECT
			user_id,
			username,
			avatar,
			connected_at,
			last_seen,
			latency
		FROM bot_status
		WHERE id = 1
	`).Scan(
		&userID,
		&username,
		&avatar,
		&connectedAt,
		&lastSeen,
		&latency,
	)

	if err != nil {
		return BotStatus{}
	}

	return BotStatus{
		UserID:      snowflake.ID(userID),
		Username:    username,
		Avatar:      avatar,
		ConnectedAt: time.Unix(connectedAt, 0),
		LastSeen:    time.Unix(lastSeen, 0),
		Latency:     time.Duration(latency) * time.Millisecond,
	}
}

func GetDatabaseCounts() DatabaseCounts {
	var counts DatabaseCounts

	err := _Database.QueryRow(`
		SELECT
			(SELECT COUNT(*) FROM guilds),
			(SELECT COUNT(*) FROM user_timezones),
			(SELECT COUNT(DISTINCT anime_id) FROM tracked_anime),
			(SELECT COUNT(*) FROM rss_feeds)
	`).Scan(
		&counts.Guilds,
		&counts.Timezones,
		&counts.Anime,
		&counts.RSSFeeds,
	)

	if err != nil {
		return DatabaseCounts{}
	}

	return counts
}
