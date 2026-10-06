package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/disgoorg/disgo/discord"
)

type guildFeature struct {
	name  string
	value int64
}

const (
	featureAnime int64 = 1 << iota
	featureRSS
)

var guildFeatureNames = []guildFeature{
	{name: "Anime", value: featureAnime},
	{name: "RSS", value: featureRSS},
}

func allGuildFeatureNames() []string {
	names := make([]string, 0, len(guildFeatureNames))
	for _, feature := range guildFeatureNames {
		names = append(names, feature.name)
	}
	return names
}

func guildFeatureMask(names []string) (int64, bool) {
	var mask int64
	for _, name := range names {
		found := false
		for _, feature := range guildFeatureNames {
			if name == feature.name {
				mask |= feature.value
				found = true
				break
			}
		}
		if !found {
			return 0, false
		}
	}
	return mask, true
}

func hasGuildFeature(features []string, name string) bool {
	for _, feature := range features {
		if feature == name {
			return true
		}
	}
	return false
}

func (guild Guild) AvailableFeatures() []string {
	names := make([]string, 0, len(guildFeatureNames))
	for _, feature := range guildFeatureNames {
		if !hasGuildFeature(guild.Features, feature.name) {
			names = append(names, feature.name)
		}
	}
	return names
}

func getGuilds(accessToken string) ([]Guild, error) {
	if accessToken == "" || _Client == nil || _Database == nil {
		return nil, fmt.Errorf("guild store unavailable")
	}

	userGuilds, err := _Client.Rest.GetCurrentUserGuilds(
		accessToken,
		0,
		0,
		200,
		false,
	)
	if err != nil {
		return nil, fmt.Errorf("get user guilds: %w", err)
	}

	featuresByID := make(map[string]int64)
	rows, err := _Database.Query(`
		SELECT guild_id, features
		FROM guilds
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id       string
			features int64
		)

		if err := rows.Scan(&id, &features); err != nil {
			return nil, err
		}
		featuresByID[id] = features
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	guilds := make([]Guild, 0, len(userGuilds))
	for _, userGuild := range userGuilds {
		id := userGuild.ID.String()
		features, ok := featuresByID[id]
		if !ok {
			continue
		}

		guild := Guild{
			ID:       id,
			Name:     userGuild.Name,
			Features: enabledGuildFeatures(features),
			CanEdit:  userGuild.Owner || userGuild.Permissions&discord.PermissionAdministrator != 0,
		}
		if icon := userGuild.IconURL(); icon != nil {
			guild.Icon = *icon
		}
		guilds = append(guilds, guild)
	}

	return guilds, nil
}

func enabledGuildFeatures(value int64) []string {
	features := make([]string, 0, len(guildFeatureNames))
	for _, feature := range guildFeatureNames {
		if value&feature.value != 0 {
			features = append(features, feature.name)
		}
	}

	return features
}

func guildsPageData(layout LayoutData) GuildsData {
	data := GuildsData{LayoutData: layout}
	query := strings.ToLower(strings.TrimSpace(layout.SearchQuery))
	for _, guild := range layout.Guilds {
		if query == "" || strings.Contains(strings.ToLower(guild.Name), query) || strings.Contains(guild.ID, query) {
			mask, _ := guildFeatureMask(guild.Features)
			data.GuildList = append(data.GuildList, GuildFeatureData{Guild: guild, InitialFeatures: mask})
		}
	}
	return data
}

func (guild GuildFeatureData) Dirty() bool {
	mask, _ := guildFeatureMask(guild.Features)
	return guild.CanEdit && mask != guild.InitialFeatures
}

func (data GuildsData) Dirty() bool {
	for _, guild := range data.GuildList {
		if guild.Dirty() {
			return true
		}
	}
	return false
}

func guildFeatureDraft(w http.ResponseWriter, r *http.Request) (GuildsData, int, bool) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		guildRequestError(w, r, "method not allowed", http.StatusMethodNotAllowed)
		return GuildsData{}, 0, false
	}
	session := getSession(r)
	if session == nil {
		guildRequestError(w, r, "authentication required", http.StatusUnauthorized)
		return GuildsData{}, 0, false
	}
	guilds, err := getGuilds(session.AccessToken)
	if err != nil {
		slog.Error("failed to get guild permissions", "error", err)
		guildRequestError(w, r, "unable to load guild permissions; try again", http.StatusBadGateway)
		return GuildsData{}, 0, false
	}
	values := r.URL.Query()
	data := guildsPageData(LayoutData{Guilds: guilds, SearchQuery: values.Get("q")})
	submitted := make(map[string]bool)
	for _, id := range values["draft_guild_ids"] {
		submitted[id] = true
	}
	knownMask, _ := guildFeatureMask(allGuildFeatureNames())
	target := -1
	for i := range data.GuildList {
		guild := &data.GuildList[i]
		if !guild.CanEdit {
			continue
		}
		if submitted[guild.ID] {
			mask, ok := guildFeatureMask(values["features_"+guild.ID])
			initial, err := strconv.ParseInt(values.Get("initial_features_"+guild.ID), 10, 64)
			if !ok || err != nil || initial < 0 || initial&^knownMask != 0 {
				guildRequestError(w, r, "invalid feature draft", http.StatusBadRequest)
				return GuildsData{}, 0, false
			}
			guild.Features = enabledGuildFeatures(mask)
			guild.InitialFeatures = initial
		}
		if guild.ID == values.Get("guild_id") {
			target = i
		}
	}
	if target < 0 {
		guildRequestError(w, r, "administrator permission required", http.StatusForbidden)
		return GuildsData{}, 0, false
	}
	return data, target, true
}

func renderGuildForm(w http.ResponseWriter, data GuildsData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := _Templates["guilds"].ExecuteTemplate(w, "guild-features-form", data); err != nil {
		slog.Error("failed to render guild form", "error", err)
	}
}

func guildRequestError(w http.ResponseWriter, r *http.Request, message string, status int) {
	if r.Header.Get("HX-Request") != "true" {
		http.Error(w, message, status)
		return
	}
	
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("HX-Retarget", "#guild-features-status")
	w.Header().Set("HX-Reselect", "#guild-features-status")
	w.Header().Set("HX-Reswap", "outerHTML")
	w.WriteHeader(status)
	if err := _Templates["guilds"].ExecuteTemplate(w, "guild-features-status", GuildsData{Message: message}); err != nil {
		slog.Error("failed to render guild error", "error", err)
	}
}

func addGuildFeatureHandler(w http.ResponseWriter, r *http.Request) {
	data, target, ok := guildFeatureDraft(w, r)
	if !ok {
		return
	}
	data.GuildList[target].ShowOptions = r.URL.Query().Get("cancel") != "1"
	renderGuildForm(w, data)
}

func guildFeatureChipHandler(w http.ResponseWriter, r *http.Request) {
	changeGuildFeature(w, r, true)
}

func removeGuildFeatureHandler(w http.ResponseWriter, r *http.Request) {
	changeGuildFeature(w, r, false)
}

func changeGuildFeature(w http.ResponseWriter, r *http.Request, add bool) {
	data, target, ok := guildFeatureDraft(w, r)
	if !ok {
		return
	}
	name := r.URL.Query().Get("feature")
	if add {
		name = r.URL.Query().Get("new_feature")
	}
	feature, ok := guildFeatureMask([]string{name})
	if !ok {
		guildRequestError(w, r, "invalid feature", http.StatusBadRequest)
		return
	}
	guild := &data.GuildList[target]
	mask, _ := guildFeatureMask(guild.Features)
	if add {
		mask |= feature
	} else {
		mask &^= feature
	}
	guild.Features = enabledGuildFeatures(mask)
	renderGuildForm(w, data)
}

func updateGuildFeaturesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		guildRequestError(w, r, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	session := getSession(r)
	if session == nil {
		guildRequestError(w, r, "authentication required", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		guildRequestError(w, r, "invalid form", http.StatusBadRequest)
		return
	}
	guildIDs := r.PostForm["guild_ids"]
	if len(guildIDs) == 0 {
		guildRequestError(w, r, "guild_ids is required", http.StatusBadRequest)
		return
	}
	guilds, err := getGuilds(session.AccessToken)
	if err != nil {
		slog.Error("failed to get guild permissions", "error", err)
		guildRequestError(w, r, "unable to load guild permissions; try again", http.StatusBadGateway)
		return
	}
	editable := make(map[string]bool, len(guilds))
	for _, guild := range guilds {
		editable[guild.ID] = guild.CanEdit
	}
	updates := make(map[string]int64, len(guildIDs))
	for _, id := range guildIDs {
		if !editable[id] {
			guildRequestError(w, r, "administrator permission required", http.StatusForbidden)
			return
		}
		mask, ok := guildFeatureMask(r.PostForm["features_"+id])
		if !ok {
			guildRequestError(w, r, "invalid feature", http.StatusBadRequest)
			return
		}
		updates[id] = mask
	}
	if err := updateGuildFeatures(updates); err != nil {
		slog.Error("failed to update guild features", "error", err)
		guildRequestError(w, r, "failed to save guild features; try again", http.StatusInternalServerError)
		return
	}
	query := r.PostForm.Get("q")
	if r.Header.Get("HX-Request") == "true" {
		for i := range guilds {
			if mask, ok := updates[guilds[i].ID]; ok {
				guilds[i].Features = enabledGuildFeatures(mask)
			}
		}
		data := guildsPageData(LayoutData{Guilds: guilds, SearchQuery: query})
		data.Saved = true
		renderGuildForm(w, data)
		return
	}
	path := "/guilds"
	if query != "" {
		path += "?" + url.Values{"q": {query}}.Encode()
	}
	http.Redirect(w, r, path, http.StatusSeeOther)
}

func updateGuildFeatures(updates map[string]int64) error {
	tx, err := _Database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	knownMask, _ := guildFeatureMask(allGuildFeatureNames())
	for id, features := range updates {
		result, err := tx.Exec(`UPDATE guilds SET features = (features & ~?) | ? WHERE guild_id = ?`, knownMask, features, id)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return sql.ErrNoRows
		}
	}
	return tx.Commit()
}
