package main

import (
	"log/slog"
	"os"
	"strings"

	_ "modernc.org/sqlite"
)

func main() {
	ctx := &BotContext{}

	//----------
	// Register commands

	InitCommonFeatures(ctx)
	InitAnimeFeatures(ctx)

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
