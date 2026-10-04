package main

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

func main() {
	ctx := &BotContext{}

	if value, ok := os.LookupEnv("SIBYL_DEBUG"); ok {
		ctx.IsDebug, _ = strconv.ParseBool(value)
	}

	//----------
	// Register commands

	InitCommonFeatures(ctx)
	InitAnimeFeatures(ctx)

	//----------
	// Start the bot

	//----------
	// Dynamic Presence
	ctx.RegisterTimer(
		5*time.Minute,
		UpdatePresence,
	)
	ctx.RegisterFirstMessage(UpdatePresence)

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
