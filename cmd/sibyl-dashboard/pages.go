package main

import (
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

var (
	_Pages     []Page
	_Templates map[string]*template.Template
)

func InitPages() {
	_Templates = make(map[string]*template.Template)

	files, err := filepath.Glob("web/templates/pages/*.html")
	if err != nil {
		panic(err)
	}

	sort.Strings(files)

	_Pages = make([]Page, 0, len(files))

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

		_Pages = append(_Pages, Page{
			ID:    id,
			Path:  path,
			Title: title,
		})

		tmpl, err := template.ParseFiles(
			"web/templates/layout.html",
			file,
		)
		if err != nil {
			panic(err)
		}

		_Templates[id] = tmpl
	}

	tmpl, err := template.ParseFiles(
		"web/templates/layout.html",
		"web/templates/404.html",
	)
	if err != nil {
		panic(err)
	}

	_Templates["404"] = tmpl
}

func RegisterHandlers(mux *http.ServeMux) {
	mux.Handle("/static/", http.StripPrefix(
		"/static/",
		http.FileServer(http.Dir("web/static")),
	))

	mux.HandleFunc("/api/overview", overviewHandler)
	mux.HandleFunc("/", pageHandler)
}

func pageHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")

	for _, page := range _Pages {
		if r.URL.Path != page.Path {
			continue
		}

		layout := LayoutData{
			Title:       page.Title,
			Page:        page.ID,
			Pages:       _Pages,
			SearchQuery: query,
		}

		var data any

		switch page.ID {
		case "overview":
			data = OverviewData{
				LayoutData: layout,
				Dashboard:  GetDashboardData(),
			}

		case "guilds":
			data = GuildsData{
				LayoutData: layout,
			}

		case "settings":
			data = SettingsData{
				LayoutData: layout,
				Config:     LoadOrCreateDefaultConfig(),
			}

		default:
			data = layout
		}

		if err := _Templates[page.ID].ExecuteTemplate(w, "layout", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		return
	}

	render404(w)
}

func overviewHandler(w http.ResponseWriter, r *http.Request) {
	data := OverviewData{
		Dashboard: GetDashboardData(),
	}

	if err := _Templates["overview"].ExecuteTemplate(
		w,
		"overview-content",
		data,
	); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func render404(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNotFound)

	data := LayoutData{
		Title: "404",
		Pages: _Pages,
	}

	if err := _Templates["404"].ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
