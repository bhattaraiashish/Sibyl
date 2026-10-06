package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/bhattaraiashish/Sibyl/internal/config"
)

func main() {
	config.LoadEnvFile()
	InitStore(os.Getenv("DISCORD_BOT_TOKEN"))

	InitPages()
	StartDashboardUpdater()

	port := 80

	if value := os.Getenv("SIBYL_PORT"); value != "" {
		port, _ = strconv.Atoi(value)
	}

	mux := http.NewServeMux()
	RegisterHandlers(mux)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	go func() {
		log.Printf("Sibyl Dashboard listening on http://localhost:%d", port)
		if err := server.ListenAndServe(); err != nil &&
			err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()

	log.Println("Shutting down Sibyl Dashboard...")

	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown failed: %v", err)
	}
}
