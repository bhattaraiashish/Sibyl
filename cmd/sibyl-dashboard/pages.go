package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/bhattaraiashish/Sibyl/internal/config"
	"github.com/bhattaraiashish/Sibyl/internal/syncmap"
	"github.com/google/uuid"
)

type UserSession struct {
	ID              string
	AccessToken     string
	RefreshToken    string
	ExpiresAt       time.Time
	CreatedAt       time.Time
	_CachedUserData *UserData
}

type SessionCache struct {
	syncmap.Map[string, UserSession]
}

type FlashCache struct {
	syncmap.Map[string, FlashData]
}

type DiscordTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

var (
	_Pages     []Page
	_Templates map[string]*template.Template

	_SessionCache = SessionCache{
		Map: syncmap.NewMap[string, UserSession](),
	}
	_FlashCache = FlashCache{
		Map: syncmap.NewMap[string, FlashData](),
	}
	_LogPath = ""
)

const (
	_SessionCookieName     = "sibyl_session"
	_OAuthStateCookieName  = "sibyl_oauth_state"
	_OAuthReturnCookieName = "sibyl_oauth_return"
)

func InitPages() {
	_Templates = make(map[string]*template.Template)

	_LogPath = os.Getenv("LOG_PATH")

	if _LogPath == "" {
		panic("Please provide LOG_PATH in env.json")
	}

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

		_Templates[id] = template.Must(
			template.ParseFiles("web/templates/layout.html", file),
		)
	}

	_Templates["404"] = template.Must(
		template.ParseFiles(
			"web/templates/layout.html",
			"web/templates/404.html",
		),
	)

	_Templates["error"] = template.Must(
		template.ParseFiles("web/templates/error.html"),
	)
}

func RegisterHandlers(mux *http.ServeMux) {
	mux.Handle("/static/", http.StripPrefix(
		"/static/",
		http.FileServer(http.Dir("web/static")),
	))

	mux.Handle("/logs/view/", http.StripPrefix("/logs/view", http.HandlerFunc(logHandler)))

	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "web/static/favicon.ico")
	})

	mux.HandleFunc("/discord/auth", discordLoginHandler)
	mux.HandleFunc("/discord/callback", discordLoginCallbackHandler)
	mux.HandleFunc("/logout", logoutHandler)

	mux.HandleFunc("/update/settings", updateSettingsPostHandler)
	mux.HandleFunc("/api/overview", overviewHandler)
	mux.HandleFunc("/", pageHandler)
}

func discordLoginHandler(w http.ResponseWriter, r *http.Request) {
	state := uuid.NewString()

	http.SetCookie(w, &http.Cookie{
		Name:     _OAuthStateCookieName,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     _OAuthReturnCookieName,
		Value:    r.URL.Query().Get("page"),
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
	})

	authURL := "https://discord.com/oauth2/authorize?" + url.Values{
		"client_id":     {os.Getenv("DISCORD_CLIENT_ID")},
		"redirect_uri":  {os.Getenv("DISCORD_REDIRECT_URL")},
		"response_type": {"code"},
		"scope":         {"identify guilds"},
		"state":         {state},
	}.Encode()

	http.Redirect(w, r, authURL, http.StatusFound)
}

func discordLoginCallbackHandler(w http.ResponseWriter, r *http.Request) {
	returnTo := "/"
	if returnCookie, err := r.Cookie(_OAuthReturnCookieName); err == nil {
		if returnCookie.Value != "" {
			returnTo = returnCookie.Value
		}
	}

	state := r.URL.Query().Get("state")
	cookie, err := r.Cookie(_OAuthStateCookieName)
	if err != nil || state == "" || state != cookie.Value {
		renderError(
			w,
			http.StatusBadRequest,
			"Invalid OAuth state",
			"The login request is invalid or has expired.",
			returnTo,
		)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		renderError(
			w,
			http.StatusBadRequest,
			"Missing authorization code",
			"Discord did not provide an authorization code.",
			returnTo,
		)
		return
	}

	token := exchangeDiscordToken(code)
	if token == nil {
		renderError(
			w,
			http.StatusBadRequest,
			"Discord login failed",
			"Failed to authenticate with Discord.",
			returnTo,
		)
		return
	}

	createSession(w, *token)
	http.Redirect(w, r, returnTo, http.StatusSeeOther)
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	returnTo := r.URL.Query().Get("page")
	if returnTo == "" {
		returnTo = "/"
	}

	if cookie, err := r.Cookie(_SessionCookieName); err == nil {
		removeSession(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     _SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, returnTo, http.StatusSeeOther)
}

func pageHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	session := getSession(r)

	canEdit := false
	if session != nil {
		user := getCachedSessionUser(session)
		canEdit = user.IsDeveloper
	}

	for _, page := range _Pages {
		if r.URL.Path != page.Path {
			continue
		}

		layout := createLayout(session, page, query)

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
			cfg := config.LoadConfig(_Database)
			data = SettingsData{
				LayoutData: layout,
				Config: ConfigData{
					NotificationIntervalMins: int(cfg.NotificationInterval.Minutes()),
					CanEdit:                  canEdit,
				},
			}

		case "logs":
			files, err := filepath.Glob(filepath.Join(_LogPath, "*.log*"))
			if err != nil {
				files = make([]string, 0)
			}

			logs := make([]Log, 0, len(files))
			for _, path := range files {
				name := filepath.Base(path)
				logs = append(logs, Log{Name: name, Path: filepath.Join("/logs/view", name)})
			}
			data = LogsData{LayoutData: layout, Logs: logs}

		default:
			data = layout
		}

		if err := _Templates[page.ID].ExecuteTemplate(w, "layout", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		return
	}

	render404(w, r)
}

func overviewHandler(w http.ResponseWriter, r *http.Request) {
	page := Page{
		Title: "Overview",
		Path:  r.URL.Path,
	}
	data := OverviewData{
		LayoutData: createLayout(getSession(r), page, ""),
		Dashboard:  GetDashboardData(),
	}

	if err := _Templates["overview"].ExecuteTemplate(
		w,
		"overview-content",
		data,
	); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func logHandler(w http.ResponseWriter, r *http.Request) {

	fileName := r.URL.Path[1:]
	filePath := filepath.Join(_LogPath, fileName)
	file, err := os.Open(filePath)
	defer file.Close()
	if err != nil {
		render404(w, r)
		return
	}

	w.WriteHeader(http.StatusOK)

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", fileName))

	_, err = io.Copy(w, file)
}

func updateSettingsPostHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	session := getSession(r)
	hasPermission := false
	if session != nil {
		if user := getCachedSessionUser(session); user != nil {
			if user.IsDeveloper {
				// Don't depend on cached value
				hasPermission = IsBotDeveloper(user.ID)
			}
		}
	}

	if !hasPermission {
		if session != nil {
			createFlashMessage(session.ID, FlashData{
				Type:    "error",
				Message: "No permission.",
			})
		}

		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}

	notificationInterval, err := strconv.Atoi(
		r.FormValue("notification_interval"),
	)
	if err != nil || notificationInterval < 1 {
		createFlashMessage(session.ID, FlashData{
			Type:    "error",
			Message: "Invalid notification interval.",
		})

		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}

	cfg := config.SibylConfig{
		NotificationInterval: time.Duration(max(1, notificationInterval)) * time.Minute,
	}
	if config.SaveConfig(_Database, cfg) {
		createFlashMessage(session.ID, FlashData{
			Type:    "success",
			Message: "Settings saved.",
		})
	} else {
		createFlashMessage(session.ID, FlashData{
			Type:    "error",
			Message: "Failed to write to database.",
		})
	}

	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func render404(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)

	page := Page{
		Title: "Not Found",
		Path:  r.URL.Path,
	}
	data := createLayout(getSession(r), page, "")

	if err := _Templates["404"].ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func renderError(w http.ResponseWriter, status int, title, message, returnTo string) {
	w.WriteHeader(status)

	data := ErrorData{
		Title:    title,
		Message:  message,
		ReturnTo: returnTo,
	}

	if err := _Templates["error"].Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

//------------------------
// Sessions

func createSession(w http.ResponseWriter, token DiscordTokenResponse) *UserSession {
	now := time.Now()

	session := UserSession{
		ID:           uuid.NewString(),
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		CreatedAt:    now,
		ExpiresAt:    now.Add(time.Duration(token.ExpiresIn) * time.Second),
	}

	if err := InsertSession(session); err != nil {
		slog.Error("failed to insert session", "error", err)
		return nil
	}

	_SessionCache.Set(session.ID, session)

	http.SetCookie(w, &http.Cookie{
		Name:     _SessionCookieName,
		Value:    session.ID,
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
	})

	return &session
}

func getSession(r *http.Request) *UserSession {
	cookie, err := r.Cookie(_SessionCookieName)
	if err != nil {
		return nil
	}

	session, ok := _SessionCache.Get(cookie.Value)

	if !ok {
		sessionPtr := FindSession(cookie.Value)
		if sessionPtr == nil {
			return nil
		}

		session = *sessionPtr

		_SessionCache.Set(session.ID, session)
	}

	now := time.Now()

	if now.Add(time.Minute).After(session.ExpiresAt) {
		token := refreshDiscordToken(session.RefreshToken)
		if token == nil {
			removeSession(session.ID)
			return nil
		}

		session.AccessToken = token.AccessToken

		if token.RefreshToken != "" {
			session.RefreshToken = token.RefreshToken
		}

		session.ExpiresAt = now.Add(
			time.Duration(token.ExpiresIn) * time.Second,
		)

		if err := updateSession(session); err != nil {
			slog.Error("failed to update session", "error", err)
			return nil
		}
	}

	return &session
}

func updateSession(session UserSession) error {
	if err := UpdateSession(session); err != nil {
		_SessionCache.Delete(session.ID)
		return err
	}
	_SessionCache.Set(session.ID, session)
	return nil
}

func removeSession(sessionID string) {
	if err := DeleteSession(sessionID); err != nil {
		slog.Error("failed to delete session", "error", err)
	}
	_SessionCache.Delete(sessionID)
	removeFlashMessage(sessionID)
}

func getCachedSessionUser(session *UserSession) *UserData {
	if session == nil {
		return nil
	}

	now := time.Now()

	if session._CachedUserData == nil || now.After(session.ExpiresAt) {
		user := GetDiscordUser(session.AccessToken)
		if user == nil {
			return nil
		}

		session._CachedUserData = user
		_SessionCache.Set(session.ID, *session)
	}

	return session._CachedUserData
}

func createLayout(session *UserSession, page Page, query string) LayoutData {
	var flash *FlashData

	if session != nil {
		flash = getFlashMessage(session.ID)
	}

	layout := LayoutData{
		Title:       page.Title,
		Page:        page.Path,
		Pages:       _Pages,
		SearchQuery: query,
		User:        getCachedSessionUser(session),
		Flash:       flash,
	}
	return layout
}

//------------------------------------
// Flash Message

func createFlashMessage(sessionID string, flash FlashData) {
	_FlashCache.Set(sessionID, flash)
}

func getFlashMessage(sessionID string) *FlashData {
	flash, ok := _FlashCache.Get(sessionID)

	if ok {
		_FlashCache.Delete((sessionID))
	}

	if !ok {
		return nil
	}

	return &flash
}

func removeFlashMessage(sessionID string) {
	_FlashCache.Delete(sessionID)
}

//------------------------------------
// Discord

func refreshDiscordToken(refreshToken string) *DiscordTokenResponse {
	values := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}

	req, err := http.NewRequest(
		http.MethodPost,
		"https://discord.com/api/oauth2/token",
		strings.NewReader(values.Encode()),
	)
	if err != nil {
		slog.Error("failed to create Discord token refresh request", "error", err)
		return nil
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(
		os.Getenv("DISCORD_CLIENT_ID"),
		os.Getenv("DISCORD_CLIENT_SECRET"),
	)

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("failed to refresh Discord token", "error", err)
		return nil
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		slog.Error(
			"Discord token refresh failed",
			"status", response.Status,
		)
		return nil
	}

	var token DiscordTokenResponse
	if err := json.NewDecoder(response.Body).Decode(&token); err != nil {
		slog.Error("failed to decode Discord token response", "error", err)
		return nil
	}

	return &token
}

func exchangeDiscordToken(code string) *DiscordTokenResponse {
	values := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {os.Getenv("DISCORD_REDIRECT_URL")},
	}

	req, err := http.NewRequest(
		http.MethodPost,
		"https://discord.com/api/oauth2/token",
		strings.NewReader(values.Encode()),
	)
	if err != nil {
		slog.Error("failed to create Discord token request", "error", err)
		return nil
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(
		os.Getenv("DISCORD_CLIENT_ID"),
		os.Getenv("DISCORD_CLIENT_SECRET"),
	)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("failed to exchange Discord authorization code", "error", err)
		return nil
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		slog.Error(
			"Discord token exchange failed",
			"status", res.Status,
		)
		return nil
	}

	var token DiscordTokenResponse

	if err := json.NewDecoder(res.Body).Decode(&token); err != nil {
		slog.Error("failed to decode Discord token response", "error", err)
		return nil
	}

	return &token
}
