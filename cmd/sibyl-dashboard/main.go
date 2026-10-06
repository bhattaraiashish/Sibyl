package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bhattaraiashish/Sibyl/internal/config"
)

func main() {
	port := flag.Int("port", 80, "HTTP server port")
	flag.Parse()

	token := config.LoadToken()
	if token == "" {
		panic("empty token file")
	}

	InitStore(token)

	InitPages()
	StartDashboardUpdater()

	mux := http.NewServeMux()
	RegisterHandlers(mux)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", *port),
		Handler: mux,
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	go func() {
		log.Printf(
			"Sibyl Dashboard listening on http://localhost:%d",
			*port,
		)

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
