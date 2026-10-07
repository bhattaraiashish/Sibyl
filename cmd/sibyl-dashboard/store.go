package main

import (
	"database/sql"
	"errors"
	"log"
	"log/slog"
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

func InitStore(token, dbPath string) {
	var err error

	_Database = config.LoadDatabase(dbPath)

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

func InsertSession(session UserSession) error {
	_, err := _Database.Exec(`
		INSERT INTO user_sessions (
			id,
			access_token,
			refresh_token,
			expires_at,
			created_at
		)
		VALUES (?, ?, ?, ?, ?)
	`,
		session.ID,
		session.AccessToken,
		session.RefreshToken,
		session.ExpiresAt.Unix(),
		session.CreatedAt.Unix(),
	)

	return err
}

func FindSession(sessionID string) *UserSession {
	var session UserSession
	var expiresAt int64
	var createdAt int64

	err := _Database.QueryRow(`
		SELECT
			id,
			access_token,
			refresh_token,
			expires_at,
			created_at
		FROM user_sessions
		WHERE id = ?
	`, sessionID).Scan(
		&session.ID,
		&session.AccessToken,
		&session.RefreshToken,
		&expiresAt,
		&createdAt,
	)

	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			slog.Error("failed to find session", "error", err)
		}

		return nil
	}

	session.ExpiresAt = time.Unix(expiresAt, 0)
	session.CreatedAt = time.Unix(createdAt, 0)

	return &session
}

func UpdateSession(session UserSession) error {
	_, err := _Database.Exec(`
		UPDATE user_sessions
		SET
			access_token = ?,
			refresh_token = ?,
			expires_at = ?,
			created_at = ?
		WHERE id = ?
	`,
		session.AccessToken,
		session.RefreshToken,
		session.ExpiresAt.Unix(),
		session.CreatedAt.Unix(),
		session.ID,
	)

	return err
}

func DeleteSession(sessionID string) error {
	_, err := _Database.Exec(`
		DELETE FROM user_sessions
		WHERE id = ?
	`, sessionID)

	return err
}

func GetDiscordUser(accessToken string) *UserData {
	user, err := _Client.Rest.GetCurrentUser(accessToken)
	if err != nil {
		slog.Error("failed to get Discord user", "error", err)
		return nil
	}

	displayName := user.Username
	if user.GlobalName != nil {
		displayName = *user.GlobalName
	}

	avatarURL := user.AvatarURL()
	if avatarURL == nil {
		defaultAvatar := "/static/default-avatar.svg"
		avatarURL = &defaultAvatar
	}

	return &UserData{
		ID:          user.ID.String(),
		DisplayName: displayName,
		Username:    user.Username,
		AvatarURL:   *avatarURL,
	}
}
