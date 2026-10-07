package main

import (
	"flag"
	"log/slog"
	"os"
	"strconv"
	"time"

	_ "modernc.org/sqlite"

	"github.com/bhattaraiashish/Sibyl/internal/config"
)

func main() {
	env := flag.String("env", "env.json", "Environment JSON")
	db := flag.String("db", "sibyl.db", "Database Path")
	flag.Parse()

	config.LoadEnvFile(*env)

	ctx := &BotContext{}
	ctx.IsDebug, _ = strconv.ParseBool(os.Getenv("SIBYL_DEBUG"))

	//----------
	// Register commands

	InitCommonFeatures(ctx)
	InitAnimeFeatures(ctx)
	InitRSSFeatures(ctx)

	//----------
	// Dynamic Presence

	ctx.RegisterTimer(
		5*time.Minute,
		UpdatePresence,
	)
	ctx.RegisterFirstMessage(UpdatePresence)

	//----------
	// Start the bot
	if err := ctx.Login(os.Getenv("DISCORD_BOT_TOKEN"), *db); err != nil {
		slog.Error("failed to login", slog.Any("err", err))
		return
	}
}
