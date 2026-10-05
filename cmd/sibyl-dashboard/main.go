package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

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

type PageData struct {
	Title         string
	Page          string
	Pages         []Page
	SearchQuery   string
	Guilds        []Guild
	SelectedGuild Guild
	Config        config.SibylConfigFile
}

var (
	pages     []Page
	templates map[string]*template.Template
)

func loadPages() ([]Page, error) {
	files, err := filepath.Glob("web/templates/pages/*.html")
	if err != nil {
		return nil, err
	}

	sort.Strings(files)

	pages := make([]Page, 0, len(files))

	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".html")

		parts := strings.SplitN(name, "_", 2)
		if len(parts) != 2 {
			continue
		}

		title := parts[1]
		id := strings.ToLower(title)

		path := "/" + id
		if id == "overview" {
			path = "/"
		}

		pages = append(pages, Page{
			ID:    id,
			Path:  path,
			Title: title,
		})
	}

	return pages, nil
}

func initTemplates() error {
	templates = make(map[string]*template.Template)

	files, err := filepath.Glob("web/templates/pages/*.html")
	if err != nil {
		return err
	}

	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".html")

		parts := strings.SplitN(name, "_", 2)
		if len(parts) != 2 {
			continue
		}

		id := strings.ToLower(parts[1])

		tmpl, err := template.ParseFiles(
			"web/templates/layout.html",
			file,
		)
		if err != nil {
			return err
		}

		templates[id] = tmpl
	}

	return nil
}

func renderPage(w http.ResponseWriter, page Page, query string) {
	data := PageData{
		Title:       page.Title,
		Page:        page.ID,
		Pages:       pages,
		SearchQuery: query,
	}

	if page.ID == "settings" {
		data.Config = loadOrCreateDefaultConfig()
	}

	if err := templates[page.ID].ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func render404(w http.ResponseWriter) {
	tmpl, err := template.ParseFiles(
		"web/templates/layout.html",
		"web/templates/404.html",
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNotFound)

	data := PageData{
		Title: "404",
		Page:  "",
		Pages: pages,
	}

	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func pageHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")

	for _, page := range pages {
		if r.URL.Path == page.Path {
			renderPage(w, page, query)
			return
		}
	}

	render404(w)
}

func loadOrCreateDefaultConfig() config.SibylConfigFile {
	data, err := os.ReadFile("config.json")
	if err != nil {
		return createDefaultConfig()
	}

	var cfg config.SibylConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return createDefaultConfig()
	}

	return cfg
}

func createDefaultConfig() config.SibylConfigFile {
	cfg := config.SibylConfigFile{
		NotificationInterval: "1h",
	}

	data, err := json.MarshalIndent(cfg, "", "    ")
	if err == nil {
		_ = os.WriteFile("config.json", data, 0644)
	}

	return cfg
}

func main() {
	port := flag.Int("port", 80, "HTTP server port")
	flag.Parse()

	var err error

	pages, err = loadPages()
	if err != nil {
		log.Fatal(err)
	}

	if err := initTemplates(); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()

	mux.Handle("/static/", http.StripPrefix(
		"/static/",
		http.FileServer(http.Dir("web/static")),
	))
	mux.HandleFunc("/", pageHandler)

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
		log.Printf("Sibyl Dashboard listening on http://localhost:%d", *port)

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
