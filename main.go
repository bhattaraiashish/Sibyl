package main

import (
	"archive/zip"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo/events"

	_ "modernc.org/sqlite"
)

func timezoneCommand(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
	args map[string]string,
) {
	userID := event.User().ID
	timezone := args["timezone"]

	if timezone != "" {
		if _, err := time.LoadLocation(timezone); err != nil {
			SendError(
				event,
				"Invalid timezone",
				"`"+timezone+"` is not a valid IANA timezone.",
			)
			return
		}

		_, err := ctx.DB.Exec(`
			INSERT INTO user_timezones (user_id, timezone)
			VALUES (?, ?)
			ON CONFLICT(user_id)
			DO UPDATE SET timezone = excluded.timezone
		`, userID, timezone)

		if err != nil {
			slog.Error("failed to save timezone", slog.Any("err", err))
			SendError(
				event,
				"Database error",
				"Failed to save your timezone.",
			)
			return
		}

		SendSuccess(
			event,
			"Timezone updated",
			"Your timezone is now `"+timezone+"`.",
		)
		return
	}

	err := ctx.DB.QueryRow(`
		SELECT timezone
		FROM user_timezones
		WHERE user_id = ?
	`, userID).Scan(&timezone)

	if err == sql.ErrNoRows {
		SendError(
			event,
			"Timezone not set",
			"You haven't set your timezone yet.",
		)
		return
	}

	if err != nil {
		slog.Error("failed to get timezone", slog.Any("err", err))
		SendError(
			event,
			"Database error",
			"Failed to retrieve your timezone.",
		)
		return
	}

	SendSuccess(
		event,
		"Your timezone",
		"`"+timezone+"`",
	)
}

var timezoneNames []string
var timezoneOnce sync.Once

func getTimezoneNames() []string {
	timezoneOnce.Do(func() {
		path := filepath.Join(
			runtime.GOROOT(),
			"lib",
			"time",
			"zoneinfo.zip",
		)

		reader, err := zip.OpenReader(path)
		if err != nil {
			slog.Error(
				"failed to open timezone database",
				slog.Any("err", err),
			)
			return
		}
		defer reader.Close()

		for _, file := range reader.File {
			name := file.Name

			if strings.HasSuffix(name, "/") {
				continue
			}

			// zoneinfo.zip entries have this prefix.
			name = strings.TrimPrefix(name, "zoneinfo/")

			if strings.Contains(name, "/") {
				timezoneNames = append(timezoneNames, name)
			}
		}

		sort.Strings(timezoneNames)
	})

	return timezoneNames
}

func timezoneAutocomplete(
	ctx *BotContext,
	event *events.AutocompleteInteractionCreate,
	args map[string]string,
) []string {
	value := strings.ToLower(args["timezone"])

	names := getTimezoneNames()

	choices := make([]string, 0, 25)

	for _, name := range names {
		if !strings.HasPrefix(strings.ToLower(name), value) {
			continue
		}

		choices = append(choices, name)
		if len(choices) >= 25 {
			break
		}
	}

	return choices
}

func main() {
	//----------
	// Setup logging
	logFile, err := os.OpenFile(
		"sibyl.log",
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0644,
	)
	if err != nil {
		panic(err)
	}
	defer logFile.Close()

	logger := slog.New(slog.NewTextHandler(
		io.MultiWriter(os.Stdout, logFile),
		&slog.HandlerOptions{},
	))

	slog.SetDefault(logger)

	ctx := &BotContext{}

	//----------
	// Register commands

	CommandBuild(ctx, "timezone", "Set or show your timezone").
		Argument("timezone", "Your IANA timezone", false).
		Autocomplete(timezoneAutocomplete).
		Handler(timezoneCommand).
		Register()

	//----------
	// Start the bot

	data, err := os.ReadFile("token")
	if err != nil {
		panic(err)
	}

	token := strings.TrimSpace(string(data))

	err = ctx.Login(token)
	if err != nil {
		slog.Error("failed to login", slog.Any("err", err))
		return
	}
}
