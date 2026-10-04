package main

import (
	"database/sql"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"

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
			MessageBuild().Embed(discord.Embed{
				Title:       "Invalid timezone",
				Description: "`" + timezone + "` is not a valid IANA timezone.",
				Color:       ColorError,
			}).SendMessage(event)
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
			MessageBuild().Embed(discord.Embed{
				Title:       "Database error",
				Description: "Failed to save your timezone.",
				Color:       ColorError,
			}).SendMessage(event)
			return
		}

		MessageBuild().Embed(discord.Embed{
			Title:       "Timezone updated",
			Description: "Your timezone is now `" + timezone + "`.",
			Color:       ColorSuccess,
		}).SendMessage(event)
		return
	}

	err := ctx.DB.QueryRow(`
		SELECT timezone
		FROM user_timezones
		WHERE user_id = ?
	`, userID).Scan(&timezone)

	if err == sql.ErrNoRows {
		MessageBuild().Embed(discord.Embed{
			Title:       "Timezone not set",
			Description: "You haven't set your timezone yet.",
			Color:       ColorError,
		}).SendMessage(event)
		return
	}

	if err != nil {
		slog.Error("failed to get timezone", slog.Any("err", err))
		MessageBuild().Embed(discord.Embed{
			Title:       "Database error",
			Description: "Failed to retrieve your timezone.",
			Color:       ColorError,
		}).SendMessage(event)
		return
	}

	MessageBuild().Embed(discord.Embed{
		Title:       "Your timezone",
		Description: "`" + timezone + "`",
		Color:       ColorSuccess,
	}).SendMessage(event)
}

func timezoneAutocomplete(
	ctx *BotContext,
	event *events.AutocompleteInteractionCreate,
	args map[string]string,
) []AutoCompleteChoice {
	value := strings.ToLower(args["timezone"])

	choices := make([]AutoCompleteChoice, 0, 25)

	for i, name := range TimezoneNamesLowercase {
		if !strings.Contains(name, value) {
			continue
		}

		choices = append(choices, AutoCompleteChoice{
			Name:  TimezoneNames[i],
			Value: TimezoneNames[i],
		})

		if len(choices) >= 25 {
			break
		}
	}

	return choices
}

func htmlToMarkdown(value string) string {
	value = html.UnescapeString(value)

	replacements := []struct {
		old string
		new string
	}{
		{"<br>", "\n"},
		{"<br/>", "\n"},
		{"<br />", "\n"},
		{"<b>", "**"},
		{"</b>", "**"},
		{"<strong>", "**"},
		{"</strong>", "**"},
		{"<i>", "*"},
		{"</i>", "*"},
		{"<em>", "*"},
		{"</em>", "*"},
		{"<u>", "__"},
		{"</u>", "__"},
		{"<s>", "~~"},
		{"</s>", "~~"},
		{"<del>", "~~"},
		{"</del>", "~~"},
	}

	for _, replacement := range replacements {
		value = strings.ReplaceAll(
			value,
			replacement.old,
			replacement.new,
		)
	}

	return value
}

func isAnimeTracked(
	db *sql.DB,
	guildID snowflake.ID,
	animeID int,
) (bool, error) {
	var exists bool

	err := db.QueryRow(`
		SELECT EXISTS(
			SELECT 1
			FROM tracked_anime
			WHERE guild_id = ?
			AND anime_id = ?
		)
	`, guildID, animeID).Scan(&exists)

	return exists, err
}

func getAnimeColor(anime *Media) int {
	if anime.CoverImage.Color == "" {
		return 0
	}

	value := strings.TrimPrefix(anime.CoverImage.Color, "#")

	color, err := strconv.ParseUint(value, 16, 32)
	if err != nil {
		return 0
	}

	return int(color)
}

func animeSearch(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
	args map[string]string,
) {
	value := args["name"]

	client := &http.Client{}

	var anime *Media
	var err error

	id, parseErr := strconv.Atoi(value)

	if parseErr == nil {
		anime, err = GetAnime(client, id)
	} else {
		result, searchErr := SearchAnime(client, value, 1)

		if searchErr != nil {
			MessageBuild().Embed(discord.Embed{
				Title:       "Search failed",
				Description: "Could not search for that anime.",
				Color:       ColorError,
			}).SendMessage(event)
			return
		}

		if len(result.Media) == 0 {
			MessageBuild().Embed(discord.Embed{
				Title:       "Anime not found",
				Description: "Could not find that anime.",
				Color:       ColorError,
			}).SendMessage(event)
			return
		}

		anime, err = GetAnime(client, result.Media[0].Id)
	}

	if err != nil {
		slog.Error(
			"failed to get anime",
			slog.Any("err", err),
		)

		MessageBuild().Embed(discord.Embed{
			Title:       "Failed to get anime",
			Description: "An error occurred while getting the anime.",
			Color:       ColorError,
		}).SendMessage(event)
		return
	}

	title := anime.Title.Romaji

	if anime.Title.English != "" &&
		anime.Title.English != anime.Title.Romaji {
		title = anime.Title.English +
			" (" + anime.Title.Romaji + ")"
	}

	description := anime.Description

	if description == "" {
		description = "No description available."
	} else {
		description = htmlToMarkdown(description)
	}

	tracking := "No"

	if anime.Status == "RELEASING" || anime.Status == "NOT_YET_RELEASED" {
		tracked, err := isAnimeTracked(
			ctx.DB,
			*event.GuildID(),
			anime.Id,
		)
		if err != nil {
			slog.Error("failed to check anime tracking", slog.Any("err", err))
		}

		if tracked {
			tracking = "Yes"
		}
	}

	startDate := ""

	if anime.StartDate.Year != 0 {
		startDate = strconv.Itoa(anime.StartDate.Year)

		if anime.StartDate.Month != 0 {
			startDate += "-" + fmt.Sprintf("%02d", anime.StartDate.Month)
		}

		if anime.StartDate.Day != 0 {
			startDate += "-" + fmt.Sprintf("%02d", anime.StartDate.Day)
		}
	}

	studio := ""
	if len(anime.Studios.Nodes) > 0 {
		studio = anime.Studios.Nodes[0].Name
	}

	episodes := strconv.Itoa(anime.Episodes)
	if anime.NextAiringEpisode != nil {
		aired := anime.NextAiringEpisode.Episode - 1
		episodes = strconv.Itoa(aired) + "/" + episodes
	}

	embed := discord.NewEmbed().
		WithTitle(title).
		WithDescription(description).
		WithColor(getAnimeColor(anime)).
		WithThumbnail(anime.CoverImage.Medium).
		AddField("Status", anime.Status, true).
		AddField("Episodes", episodes, true).
		AddField("Rating", strconv.Itoa(anime.AverageScore)+"/100", true).
		AddField("Start Date", startDate, true).
		AddField("Studio", studio, true)

	if anime.Status == "RELEASING" || anime.Status == "NOT_YET_RELEASED" {
		embed = embed.AddField("Tracking", tracking, true)
	}

	message := discord.NewMessageCreate().
		WithEmbeds(embed)

	if anime.Status == "RELEASING" || anime.Status == "NOT_YET_RELEASED" {
		message = message.WithComponents(
			discord.NewActionRow(
				discord.NewSuccessButton(
					"Track",
					ComponentID("track", strconv.Itoa(anime.Id)),
				),
				discord.NewDangerButton(
					"Untrack",
					ComponentID("untrack", strconv.Itoa(anime.Id)),
				),
			),
		)
	}

	err = event.CreateMessage(message)

	if err != nil {
		slog.Error(
			"failed to send anime message",
			slog.Any("err", err),
		)
	}
}

func animeSearchAutocomplete(
	ctx *BotContext,
	event *events.AutocompleteInteractionCreate,
	args map[string]string,
) []AutoCompleteChoice {
	value := strings.ToLower(args["name"])

	choices := make([]AutoCompleteChoice, 0, 25)

	if value == "" {
		return choices
	}

	result, err := SearchAnime(
		&http.Client{},
		value,
		25,
	)
	if err != nil {
		slog.Error(
			"failed to search anime",
			slog.Any("err", err),
		)
		return choices
	}

	for _, anime := range result.Media {
		english := anime.Title.English
		romaji := anime.Title.Romaji

		name := romaji

		if english != "" && english != romaji {
			name = english + " (" + romaji + ")"
		}

		if utf8.RuneCountInString(name) > 100 {
			name = string([]rune(name)[:100])
		}

		choices = append(choices, AutoCompleteChoice{
			Name:  name,
			Value: strconv.Itoa(anime.Id),
		})
	}

	return choices
}

func toggleTrackAnime(
	ctx *BotContext,
	event *events.ComponentInteractionCreate,
	args []string,
	track bool,
) {
	if len(args) < 1 {
		return
	}

	id, err := strconv.Atoi(args[0])
	if err != nil {
		return
	}

	if !HasRole(ctx, event.Member().Member, *event.GuildID(), "anime") {
		MessageBuild().Embed(discord.Embed{
			Title:       "Permission denied",
			Description: "You need the `anime` role to track anime.",
			Color:       ColorError,
		}).SendComponent(event)
		return
	}

	anime, err := GetAnime(&http.Client{}, id)
	if err != nil {
		slog.Error(
			"failed to get anime",
			slog.Any("err", err),
		)

		MessageBuild().Embed(discord.Embed{
			Title:       "Failed to get anime",
			Description: "An error occurred while getting the anime.",
			Color:       ColorError,
		}).SendComponent(event)
		return
	}

	guildID := event.GuildID()
	channelID := event.Channel().ID()

	if track {
		_, err = ctx.DB.Exec(`
			INSERT OR IGNORE INTO tracked_anime (
				guild_id,
				anime_id,
				channel_id
			)
			VALUES (?, ?, ?)
		`, guildID, id, channelID)

		if err != nil {
			slog.Error(
				"failed to track anime",
				slog.Any("err", err),
			)

			MessageBuild().Embed(discord.Embed{
				Title:       "Failed to track anime",
				Description: "An error occurred while trying to track the anime.",
				Color:       ColorError,
			}).SendComponent(event)
			return
		}

		MessageBuild().Embed(discord.Embed{
			Title:       "Anime tracked",
			Description: anime.Title.Romaji + " is now being tracked.",
			Color:       ColorSuccess,
			Thumbnail: &discord.EmbedResource{
				URL: anime.CoverImage.Medium,
			},
		}).SendComponent(event)
	} else {
		_, err = ctx.DB.Exec(`
			DELETE FROM tracked_anime
			WHERE guild_id = ?
			AND anime_id = ?
		`, guildID, id)

		if err != nil {
			slog.Error(
				"failed to untrack anime",
				slog.Any("err", err),
			)

			MessageBuild().Embed(discord.Embed{
				Title:       "Failed to untrack anime",
				Description: "An error occurred while trying to untrack the anime.",
				Color:       ColorError,
			}).SendComponent(event)
			return
		}

		MessageBuild().Embed(discord.Embed{
			Title:       "Anime untracked",
			Description: anime.Title.Romaji + " is no longer being tracked.",
			Color:       ColorWarning,
			Thumbnail: &discord.EmbedResource{
				URL: anime.CoverImage.Medium,
			},
		}).SendComponent(event)
	}
}

func trackAnime(
	ctx *BotContext,
	event *events.ComponentInteractionCreate,
	args []string,
) {
	toggleTrackAnime(ctx, event, args, true)
}

func untrackAnime(
	ctx *BotContext,
	event *events.ComponentInteractionCreate,
	args []string,
) {
	toggleTrackAnime(ctx, event, args, false)
}

func getTrackedAnime(
	ctx *BotContext,
	guildID snowflake.ID,
) ([]*Media, error) {
	rows, err := ctx.DB.Query(`
		SELECT anime_id
		FROM tracked_anime
		WHERE guild_id = ?
		ORDER BY anime_id
	`, guildID)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	client := &http.Client{}

	var animeList []*Media

	for rows.Next() {
		var id int

		if err := rows.Scan(&id); err != nil {
			return nil, err
		}

		anime, err := GetAnime(client, id)
		if err != nil {
			slog.Error(
				"failed to get tracked anime",
				slog.Any("err", err),
				slog.Int("anime_id", id),
			)
			continue
		}

		animeList = append(animeList, anime)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return animeList, nil
}

func trackedAnime(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
	args map[string]string,
) {
	animeList, err := getTrackedAnime(
		ctx,
		*event.GuildID(),
	)

	if err != nil {
		slog.Error(
			"failed to get tracked anime",
			slog.Any("err", err),
		)

		MessageBuild().Embed(discord.Embed{
			Title:       "Failed to get tracked anime",
			Description: "An error occurred while getting tracked anime.",
			Color:       ColorError,
		}).SendMessage(event)
		return
	}

	if len(animeList) == 0 {
		MessageBuild().Embed(discord.Embed{
			Title:       "Tracked Anime",
			Description: "No anime is currently being tracked.",
			Color:       ColorWarning,
		}).SendMessage(event)
		return
	}

	days := []time.Weekday{
		time.Monday,
		time.Tuesday,
		time.Wednesday,
		time.Thursday,
		time.Friday,
		time.Saturday,
		time.Sunday,
	}

	groups := make(map[time.Weekday][]string)

	for _, anime := range animeList {
		if anime.NextAiringEpisode == nil {
			continue
		}

		title := anime.Title.Romaji

		if anime.Title.English != "" &&
			anime.Title.English != anime.Title.Romaji {
			title = anime.Title.English +
				" (" + anime.Title.Romaji + ")"
		}

		episodes := strconv.Itoa(anime.Episodes)

		aired := anime.NextAiringEpisode.Episode - 1
		episodes = strconv.Itoa(aired) + "/" + episodes

		airingAt := time.Unix(
			anime.NextAiringEpisode.AiringAt,
			0,
		)

		groups[airingAt.Weekday()] = append(
			groups[airingAt.Weekday()],
			fmt.Sprintf(
				"%s — %s",
				title,
				episodes,
			),
		)
	}

	var list []string

	for _, day := range days {
		anime := groups[day]
		if len(anime) == 0 {
			continue
		}

		var dayList []string

		for i, title := range anime {
			dayList = append(
				dayList,
				fmt.Sprintf("%d. %s", i+1, title),
			)
		}

		list = append(
			list,
			fmt.Sprintf(
				"**%s**\n%s",
				day,
				strings.Join(dayList, "\n"),
			),
		)
	}

	MessageBuild().Embed(discord.Embed{
		Title:       "Tracked Anime",
		Description: strings.Join(list, "\n\n"),
		Color:       ColorSuccess,
	}).SendMessage(event)
}

func todayAnime(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
	args map[string]string,
) {
	var timezone string

	err := ctx.DB.QueryRow(`
		SELECT timezone
		FROM user_timezones
		WHERE user_id = ?
	`, event.Member().User.ID).Scan(&timezone)

	if err != nil {
		timezone = "UTC"
		if err != sql.ErrNoRows {
			slog.Error(
				"failed to get timezone",
				slog.Any("err", err),
			)
		}
	}

	location, err := time.LoadLocation(timezone)
	if err != nil {
		MessageBuild().Embed(discord.Embed{
			Title:       "Invalid timezone",
			Description: "Your configured timezone is invalid.",
			Color:       ColorError,
		}).SendMessage(event)
		return
	}

	now := time.Now().In(location)
	start := now.Add(-12 * time.Hour)
	end := now.Add(12 * time.Hour)

	animeList, err := getTrackedAnime(
		ctx,
		*event.GuildID(),
	)

	if err != nil {
		slog.Error(
			"failed to get tracked anime",
			slog.Any("err", err),
		)

		MessageBuild().Embed(discord.Embed{
			Title:       "Failed to get tracked anime",
			Description: "An error occurred while getting tracked anime.",
			Color:       ColorError,
		}).SendMessage(event)
		return
	}

	var list []string

	for _, anime := range animeList {
		if anime.NextAiringEpisode == nil {
			continue
		}

		airingAt := time.Unix(
			anime.NextAiringEpisode.AiringAt,
			0,
		).In(location)

		if airingAt.Before(start) || airingAt.After(end) {
			continue
		}

		title := anime.Title.Romaji

		if anime.Title.English != "" &&
			anime.Title.English != anime.Title.Romaji {
			title = anime.Title.English +
				" (" + anime.Title.Romaji + ")"
		}

		list = append(
			list,
			fmt.Sprintf(
				"%d. %s — Episode %d",
				len(list)+1,
				title,
				anime.NextAiringEpisode.Episode,
			),
		)
	}

	if len(list) == 0 {
		MessageBuild().Embed(discord.Embed{
			Title:       "Today",
			Description: "No tracked anime airing within ±12 hours.",
			Color:       ColorWarning,
		}).SendMessage(event)
		return
	}

	MessageBuild().Embed(discord.Embed{
		Title:       "Today",
		Description: strings.Join(list, "\n"),
		Color:       ColorSuccess,
		Footer: &discord.EmbedFooter{
			Text: "Timezone: " + timezone,
		},
	}).SendMessage(event)
}

func currentAnimeSeason() (string, int) {
	now := time.Now()

	month := now.Month()

	switch {
	case month >= time.January && month <= time.March:
		return "WINTER", now.Year()
	case month >= time.April && month <= time.June:
		return "SPRING", now.Year()
	case month >= time.July && month <= time.September:
		return "SUMMER", now.Year()
	default:
		return "FALL", now.Year()
	}
}

func formatSeason(season string) string {
	switch season {
	case "WINTER":
		return "Winter"
	case "SPRING":
		return "Spring"
	case "SUMMER":
		return "Summer"
	case "FALL":
		return "Fall"
	default:
		return season
	}
}

func seasonAnime(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
	args map[string]string,
) {
	client := &http.Client{}

	season, year := currentAnimeSeason()

	result, err := SearchSeasonAnime(
		client,
		season,
		year,
		50,
	)

	if err != nil {
		slog.Error(
			"failed to search season anime",
			slog.Any("err", err),
		)

		MessageBuild().
			Embed(
				discord.NewEmbed().
					WithTitle("Search failed").
					WithDescription(
						"Could not get this season's anime.",
					).
					WithColor(ColorError),
			).
			SendMessage(event)

		return
	}

	if len(result.Media) == 0 {
		MessageBuild().
			Embed(
				discord.NewEmbed().
					WithTitle("No anime").
					WithDescription(
						"No releasing anime found for this season.",
					).
					WithColor(ColorWarning),
			).
			SendMessage(event)

		return
	}

	var list []string

	for i, anime := range result.Media {
		title := anime.Title.Romaji

		if anime.Title.English != "" &&
			anime.Title.English != anime.Title.Romaji {
			title = anime.Title.English +
				" (" + anime.Title.Romaji + ")"
		}

		list = append(
			list,
			fmt.Sprintf("%d. %s", i+1, title),
		)
	}

	MessageBuild().
		Embed(
			discord.NewEmbed().
				WithTitle(
					fmt.Sprintf(
						"%s %d Anime",
						formatSeason(season),
						year,
					),
				).
				WithDescription(
					strings.Join(list, "\n"),
				).
				WithColor(ColorSuccess),
		).
		SendMessage(event)
}

func enableAnimeNotifications(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
	value string,
) {
	interval := 30 * time.Minute

	if value != "" {
		var err error

		interval, err = time.ParseDuration(value)
		if err != nil || interval <= 0 {
			MessageBuild().
				Embed(
					discord.NewEmbed().
						WithTitle("Invalid interval").
						WithDescription(
							"Use a duration such as `30m`, `1h`, or `2h30m`.",
						).
						WithColor(ColorError),
				).
				SendMessage(event)
			return
		}
	}

	_, err := ctx.DB.Exec(`
		INSERT INTO guild_anime_notifications (
			guild_id,
			interval,
			last_checked
		)
		VALUES (?, ?, 0)
		ON CONFLICT(guild_id)
		DO UPDATE SET
			interval = excluded.interval
	`,
		event.GuildID(),
		int64(interval.Seconds()),
	)

	if err != nil {
		slog.Error(
			"failed to enable anime notifications",
			slog.Any("err", err),
			slog.String(
				"guild_id",
				event.GuildID().String(),
			),
		)

		MessageBuild().
			Embed(
				discord.NewEmbed().
					WithTitle("Database error").
					WithDescription(
						"Failed to enable anime notifications.",
					).
					WithColor(ColorError),
			).
			SendMessage(event)
		return
	}

	MessageBuild().
		Embed(
			discord.NewEmbed().
				WithTitle("Anime notifications enabled").
				WithDescription(
					fmt.Sprintf(
						"Anime episodes will be checked every `%s`.",
						interval,
					),
				).
				WithColor(ColorSuccess),
		).
		SendMessage(event)
}

func disableAnimeNotifications(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
) {
	_, err := ctx.DB.Exec(`
		DELETE FROM guild_anime_notifications
		WHERE guild_id = ?
	`, event.GuildID())

	if err != nil {
		slog.Error(
			"failed to disable anime notifications",
			slog.Any("err", err),
			slog.String(
				"guild_id",
				event.GuildID().String(),
			),
		)

		MessageBuild().
			Embed(
				discord.NewEmbed().
					WithTitle("Database error").
					WithDescription(
						"Failed to disable anime notifications.",
					).
					WithColor(ColorError),
			).
			SendMessage(event)
		return
	}

	MessageBuild().
		Embed(
			discord.NewEmbed().
				WithTitle("Anime notifications disabled").
				WithColor(ColorWarning),
		).
		SendMessage(event)
}

func notifyAnimeCommand(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
	args map[string]string,
) {
	if !HasRole(ctx, event.Member().Member, *event.GuildID(), "anime") {
		MessageBuild().Embed(discord.Embed{
			Title:       "Permission denied",
			Description: "You need the `anime` role to configure anime notifications.",
			Color:       ColorError,
		}).SendMessage(event)
		return
	}

	switch args["action"] {
	case "enable":
		enableAnimeNotifications(
			ctx,
			event,
			args["interval"],
		)

	case "disable":
		disableAnimeNotifications(
			ctx,
			event,
		)

	default:
		MessageBuild().
			Embed(
				discord.NewEmbed().
					WithTitle("Invalid action").
					WithDescription(
						"Use `enable` or `disable`.",
					).
					WithColor(ColorError),
			).
			SendMessage(event)
	}
}

func notifyAnimeEpisode(
	ctx *BotContext,
	channelID snowflake.ID,
	anime *Media,
	episode int,
) {
	title := anime.Title.Romaji

	if anime.Title.English != "" &&
		anime.Title.English != anime.Title.Romaji {
		title = anime.Title.English + " (" + anime.Title.Romaji + ")"
	}

	MessageBuild().
		Embed(
			discord.NewEmbed().
				WithTitle("New episode released").
				WithDescription(
					fmt.Sprintf(
						"%s — Episode %d",
						title,
						episode,
					),
				).
				WithThumbnail(anime.CoverImage.Medium).
				WithColor(getAnimeColor(anime)),
		).
		SendChannel(ctx, channelID)
}

func checkTrackedAnime(ctx *BotContext) {
	type NotificationSetting struct {
		guildID     snowflake.ID
		interval    int64
		lastChecked int64
	}

	type TrackedAnime struct {
		animeID             int
		channelID           snowflake.ID
		lastNotifiedEpisode int
	}

	rows, err := ctx.DB.Query(`
		SELECT
			guild_id,
			interval,
			last_checked
		FROM guild_anime_notifications
	`)
	if err != nil {
		slog.Error(
			"failed to query anime notification settings",
			slog.Any("err", err),
		)
		return
	}

	var settings []NotificationSetting

	for rows.Next() {
		var setting NotificationSetting

		if err := rows.Scan(
			&setting.guildID,
			&setting.interval,
			&setting.lastChecked,
		); err != nil {
			slog.Error(
				"failed to scan anime notification settings",
				slog.Any("err", err),
			)
			continue
		}

		settings = append(settings, setting)
	}

	if err := rows.Err(); err != nil {
		slog.Error(
			"failed while reading anime notification settings",
			slog.Any("err", err),
		)
	}

	rows.Close()

	now := time.Now().Unix()
	client := &http.Client{}

	for _, setting := range settings {
		if now-setting.lastChecked < setting.interval {
			continue
		}

		animeRows, err := ctx.DB.Query(`
			SELECT
				anime_id,
				channel_id,
				last_notified_episode
			FROM tracked_anime
			WHERE guild_id = ?
		`, setting.guildID)

		if err != nil {
			slog.Error(
				"failed to query tracked anime",
				slog.Any("err", err),
				slog.String("guild_id", setting.guildID.String()),
			)
			continue
		}

		var trackedAnime []TrackedAnime

		for animeRows.Next() {
			var anime TrackedAnime

			if err := animeRows.Scan(
				&anime.animeID,
				&anime.channelID,
				&anime.lastNotifiedEpisode,
			); err != nil {
				slog.Error(
					"failed to scan tracked anime",
					slog.Any("err", err),
					slog.String("guild_id", setting.guildID.String()),
				)
				continue
			}

			trackedAnime = append(trackedAnime, anime)
		}

		if err := animeRows.Err(); err != nil {
			slog.Error(
				"failed while reading tracked anime",
				slog.Any("err", err),
				slog.String("guild_id", setting.guildID.String()),
			)
		}

		animeRows.Close()

		// The SELECT above is finished and closed before any writes.
		for _, tracked := range trackedAnime {
			anime, err := GetAnime(client, tracked.animeID)
			if err != nil {
				slog.Error(
					"failed to get anime",
					slog.Any("err", err),
					slog.Int("anime_id", tracked.animeID),
				)
				continue
			}

			if anime.NextAiringEpisode == nil {
				continue
			}

			airedEpisode := anime.NextAiringEpisode.Episode - 1

			if airedEpisode <= tracked.lastNotifiedEpisode {
				continue
			}

			for episode := tracked.lastNotifiedEpisode + 1; episode <= airedEpisode; episode++ {
				notifyAnimeEpisode(
					ctx,
					tracked.channelID,
					anime,
					episode,
				)
			}

			_, err = ctx.DB.Exec(`
				UPDATE tracked_anime
				SET last_notified_episode = ?
				WHERE guild_id = ?
				AND anime_id = ?
			`,
				airedEpisode,
				setting.guildID,
				tracked.animeID,
			)

			if err != nil {
				slog.Error(
					"failed to update last notified episode",
					slog.Any("err", err),
					slog.String("guild_id", setting.guildID.String()),
					slog.Int("anime_id", tracked.animeID),
				)
			}
		}

		_, err = ctx.DB.Exec(`
			UPDATE guild_anime_notifications
			SET last_checked = ?
			WHERE guild_id = ?
		`, now, setting.guildID)

		if err != nil {
			slog.Error(
				"failed to update anime notification check time",
				slog.Any("err", err),
				slog.String("guild_id", setting.guildID.String()),
			)
		}
	}
}

func main() {
	ctx := &BotContext{}

	//----------
	// Register commands

	CommandBuild(ctx, "timezone", "Set or show your timezone").
		Argument("timezone", "Your IANA timezone", false).
		Autocomplete(timezoneAutocomplete).
		Handler(timezoneCommand).
		Register()

	CommandBuild(ctx, "anime", "Search for an anime").
		Argument("name", "The name of the anime", true).
		Autocomplete(animeSearchAutocomplete).
		Handler(animeSearch).
		Register()

	CommandBuild(ctx, "tracked", "Show tracked anime").
		Handler(trackedAnime).
		Register()

	CommandBuild(ctx, "season", "Show anime for the current season").
		Handler(seasonAnime).
		Register()

	CommandBuild(ctx, "today", "Show anime airing today").
		Handler(todayAnime).
		Register()

	CommandBuild(ctx, "notify-anime", "Configure anime episode notifications").
		Argument("action", "Enable or disable notifications", true).
		Argument("interval", "Check interval, such as 30m or 1h", false).
		Handler(notifyAnimeCommand).
		Register()

	ctx.RegisterComponent("track", trackAnime)
	ctx.RegisterComponent("untrack", untrackAnime)

	ctx.RegisterTimer(
		time.Minute,
		checkTrackedAnime,
	)

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
