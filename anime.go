package main

import (
	"database/sql"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
)

func InitAnimeFeatures(ctx *BotContext) {
	CommandBuild(ctx, "anime", "Search for an anime").
		Argument("name", "The name of the anime", true).
		Autocomplete(AnimeSearchAutocomplete).
		Handler(AnimeSearch).
		Feature(FeatureAnime).
		Register()

	CommandBuild(ctx, "tracked", "Show tracked anime").
		Handler(TrackedAnime).
		Feature(FeatureAnime).
		Register()

	CommandBuild(ctx, "season", "Show anime for the current season").
		Handler(SeasonAnime).
		Feature(FeatureAnime).
		Register()

	CommandBuild(ctx, "today", "Show anime airing today").
		Handler(TodayAnime).
		Feature(FeatureAnime).
		Register()

	CommandBuild(ctx, "next", "Show next upcoming anime").
		Handler(NextAnime).
		Feature(FeatureAnime).
		Register()

	CommandBuild(ctx, "notify-anime", "Configure anime episode notifications").
		Argument("action", "Enable or disable notifications", true).
		Autocomplete(EnableDisableAutocomplete).
		Handler(NotifyAnimeCommand).
		Feature(FeatureAnime).
		Register()

	CommandBuild(ctx, "track", "Track an anime").
		Argument("name", "Anime to track", true).
		Autocomplete(AnimeSearchAutocomplete).
		Handler(TrackAnime).
		Feature(FeatureAnime).
		Register()

	CommandBuild(ctx, "untrack", "Stop tracking an anime").
		Argument("name", "Anime to untrack", true).
		Autocomplete(AnimeSearchAutocomplete).
		Handler(UntrackAnime).
		Feature(FeatureAnime).
		Register()

	ctx.RegisterComponent("track", TrackAnimeComponent)
	ctx.RegisterComponent("untrack", UntrackAnimeComponent)

	ctx.RegisterTimer(
		time.Minute,
		CheckTrackedAnime,
	)
}

func AnimeSearch(
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
			}).SendMessage(ctx, event)
			return
		}

		if len(result.Media) == 0 {
			MessageBuild().Embed(discord.Embed{
				Title:       "Anime not found",
				Description: "Could not find that anime.",
				Color:       ColorError,
			}).SendMessage(ctx, event)
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
		}).SendMessage(ctx, event)
		return
	}

	title := getAnimeTitle(anime)

	description := anime.Description

	if description == "" {
		description = "No description available."
	} else {
		description = htmlToMarkdown(description)
	}

	if len(description) > 4096 {
		description = string([]rune(description)[:4096])
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
	nextEpisode := "N/A"

	if anime.NextAiringEpisode != nil {
		airingAt := time.Unix(
			anime.NextAiringEpisode.AiringAt,
			0,
		)

		aired := anime.NextAiringEpisode.Episode - 1
		episodes = strconv.Itoa(aired) + "/" + episodes
		nextEpisode = fmt.Sprintf(
			"Episode %d — %s",
			anime.NextAiringEpisode.Episode,
			formatCountdown(airingAt),
		)
	}

	embed := discord.NewEmbed().
		WithTitle(title).
		WithURL(fmt.Sprintf("https://anilist.co/anime/%d", anime.Id)).
		WithDescription(description).
		WithColor(getAnimeColor(anime)).
		WithThumbnail(anime.CoverImage.Medium).
		AddField("Status", anime.Status, true).
		AddField("Episodes", episodes, true).
		AddField("Next Episode", nextEpisode, true).
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

func AnimeSearchAutocomplete(
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
		name := getAnimeMinimalTitle(&anime)

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

func TrackedAnime(
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
		}).SendMessage(ctx, event)
		return
	}

	if len(animeList) == 0 {
		MessageBuild().Embed(discord.Embed{
			Title:       "Tracked Anime",
			Description: "No anime is currently being tracked.",
			Color:       ColorWarning,
		}).SendMessage(ctx, event)
		return
	}

	var upcoming []*Media

	for _, anime := range animeList {
		if anime.NextAiringEpisode == nil {
			continue
		}

		upcoming = append(upcoming, anime)
	}

	sort.Slice(
		upcoming,
		func(i, j int) bool {
			return upcoming[i].NextAiringEpisode.AiringAt <
				upcoming[j].NextAiringEpisode.AiringAt
		},
	)

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
	var comingSoon []string

	for _, anime := range upcoming {
		if anime.NextAiringEpisode == nil {
			continue
		}

		title := getAnimeTitle(anime)
		title = fmt.Sprintf(
			"[%s](https://anilist.co/anime/%d)",
			title,
			anime.Id,
		)

		if anime.Status == "NOT_YET_RELEASED" {
			releaseDate := time.Date(
				anime.StartDate.Year,
				time.Month(anime.StartDate.Month),
				anime.StartDate.Day,
				0, 0, 0, 0,
				time.UTC,
			)

			comingSoon = append(
				comingSoon,
				fmt.Sprintf(
					"%s — %d episodes — %s",
					title,
					anime.Episodes,
					releaseDate.Format("Jan 2, 2006"),
				),
			)
			continue
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

	var description DescriptionBuilder

	for _, day := range days {
		anime := groups[day]
		if len(anime) == 0 {
			continue
		}

		if !description.AddLine(fmt.Sprintf("**%s**", day)) {
			break
		}

		for i, title := range anime {
			line := fmt.Sprintf("%d. %s", i+1, title)

			if !description.AddLine(line) {
				break
			}
		}
	}

	if len(comingSoon) > 0 && !description.Truncated() {
		if description.AddLine("**Coming Soon**") {
			for i, title := range comingSoon {
				line := fmt.Sprintf("%d. %s", i+1, title)

				if !description.AddLine(line) {
					break
				}
			}
		}
	}

	embed := discord.NewEmbed().
		WithTitle("Tracked Anime").
		WithDescription(description.String()).
		WithColor(ColorSuccess)

	if description.Truncated() {
		embed = embed.WithFooter("Results truncated!", "")
	}

	MessageBuild().
		Embed(embed).
		SendMessage(ctx, event)
}

func TodayAnime(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
	args map[string]string,
) {
	upcoming, timezone, err := getUpcomingAnime(
		ctx,
		*event.GuildID(),
		event.Member().User.ID,
	)

	if err != nil {
		slog.Error(
			"failed to get upcoming anime",
			slog.Any("err", err),
		)

		MessageBuild().
			Embed(
				discord.NewEmbed().
					WithTitle("Failed to get today's anime").
					WithDescription(
						"An error occurred while getting today's anime.",
					).
					WithColor(ColorError),
			).
			SendMessage(ctx, event)
		return
	}

	location := time.UTC

	if len(upcoming) > 0 {
		location = upcoming[0].AiringAt.Location()
	}

	now := time.Now().In(location)
	start := now.Add(-12 * time.Hour)
	end := now.Add(12 * time.Hour)

	var description DescriptionBuilder

	for _, episode := range upcoming {
		if episode.AiringAt.Before(start) ||
			episode.AiringAt.After(end) {
			continue
		}

		title := getAnimeTitle(episode.Anime)
		title = fmt.Sprintf(
			"[%s](https://anilist.co/anime/%d)",
			title,
			episode.Anime.Id,
		)

		line := fmt.Sprintf(
			"%d. %s — Episode %d — %s",
			description.Len()+1,
			title,
			episode.Episode,
			episode.AiringAt.Format("15:04"),
		)

		if !description.AddLine(line) {
			break
		}
	}

	if description.Len() == 0 {
		MessageBuild().
			Embed(
				discord.NewEmbed().
					WithTitle("Today's Anime").
					WithDescription(
						"No tracked anime airing within ±12 hours.",
					).
					WithColor(ColorWarning).
					WithFooter(
						"Timezone: "+timezone,
						"",
					),
			).
			SendMessage(ctx, event)
		return
	}

	footer := "Timezone: " + timezone

	if description.Truncated() {
		footer += " • Results truncated!"
	}

	MessageBuild().
		Embed(
			discord.NewEmbed().
				WithTitle("Today's Anime").
				WithDescription(description.String()).
				WithColor(ColorSuccess).
				WithFooter(
					footer,
					"",
				),
		).
		SendMessage(ctx, event)
}

func NextAnime(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
	args map[string]string,
) {
	upcoming, timezone, err := getUpcomingAnime(
		ctx,
		*event.GuildID(),
		event.Member().User.ID,
	)

	if err != nil {
		slog.Error(
			"failed to get upcoming anime",
			slog.Any("err", err),
		)

		MessageBuild().
			Embed(
				discord.NewEmbed().
					WithTitle("Failed to get upcoming anime").
					WithDescription(
						"An error occurred while getting upcoming anime.",
					).
					WithColor(ColorError),
			).
			SendMessage(ctx, event)
		return
	}

	if len(upcoming) == 0 {
		MessageBuild().
			Embed(
				discord.NewEmbed().
					WithTitle("Next Episodes").
					WithDescription(
						"No upcoming episodes for tracked anime.",
					).
					WithColor(ColorWarning).
					WithFooter(
						"Timezone: "+timezone,
						"",
					),
			).
			SendMessage(ctx, event)
		return
	}

	now := time.Now().In(upcoming[0].AiringAt.Location())

	var description DescriptionBuilder

	for _, episode := range upcoming {
		var when string

		if episode.AiringAt.Year() == now.Year() &&
			episode.AiringAt.YearDay() == now.YearDay() {
			when = "Today " + episode.AiringAt.Format("15:04")
		} else if episode.AiringAt.Year() == now.Year() &&
			episode.AiringAt.YearDay() == now.YearDay()+1 {
			when = "Tomorrow " + episode.AiringAt.Format("15:04")
		} else {
			when = episode.AiringAt.Format("Mon Jan 2 15:04")
		}

		title := getAnimeTitle(episode.Anime)
		title = fmt.Sprintf(
			"[%s](https://anilist.co/anime/%d)",
			title,
			episode.Anime.Id,
		)

		line := fmt.Sprintf(
			"%d. %s — Episode %d — %s",
			description.Len()+1,
			title,
			episode.Episode,
			when,
		)

		if !description.AddLine(line) {
			break
		}
	}

	footer := "Timezone: " + timezone

	if description.Truncated() {
		footer += " • Results truncated!"
	}

	MessageBuild().
		Embed(
			discord.NewEmbed().
				WithTitle("Next Episodes").
				WithDescription(description.String()).
				WithColor(ColorSuccess).
				WithFooter(
					footer,
					"",
				),
		).
		SendMessage(ctx, event)
}

func SeasonAnime(
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
			SendMessage(ctx, event)

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
			SendMessage(ctx, event)

		return
	}

	var description DescriptionBuilder

	for i, anime := range result.Media {
		title := getAnimeMinimalTitle(&anime)
		title = fmt.Sprintf(
			"[%s](https://anilist.co/anime/%d)",
			title,
			anime.Id,
		)

		line := fmt.Sprintf("%d. %s", i+1, title)

		if !description.AddLine(line) {
			break
		}
	}

	embed := discord.NewEmbed().
		WithTitle(
			fmt.Sprintf(
				"%s %d Anime",
				formatSeason(season),
				year,
			),
		).
		WithURL(
			fmt.Sprintf(
				"https://anilist.co/search/anime?season=%s&seasonYear=%d",
				season,
				year,
			),
		).
		WithDescription(description.String()).
		WithColor(ColorSuccess)

	if description.Truncated() {
		embed = embed.WithFooter("Results truncated!", "")
	}

	MessageBuild().
		Embed(embed).
		SendMessage(ctx, event)
}

func NotifyAnimeCommand(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
	args map[string]string,
) {
	if !HasRole(ctx, &event.Member().Member, *event.GuildID(), "anime") {
		MessageBuild().Embed(discord.Embed{
			Title:       "Permission denied",
			Description: "You need the `anime` role to configure anime notifications.",
			Color:       ColorError,
		}).SendMessage(ctx, event)
		return
	}

	switch args["action"] {
	case "enable":
		enableAnimeNotifications(
			ctx,
			event,
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
			SendMessage(ctx, event)
	}
}

func TrackAnime(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
	args map[string]string,
) {
	handleTrackAnime(ctx, event, args, true)
}

func UntrackAnime(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
	args map[string]string,
) {
	handleTrackAnime(ctx, event, args, false)
}

func TrackAnimeComponent(
	ctx *BotContext,
	event *events.ComponentInteractionCreate,
	args []string,
) {
	handleTrackAnimeComponent(ctx, event, args, true)
}

func UntrackAnimeComponent(
	ctx *BotContext,
	event *events.ComponentInteractionCreate,
	args []string,
) {
	handleTrackAnimeComponent(ctx, event, args, false)
}

var _AnimeCheckInterval = 30 * time.Minute

func SetAnimeCheckInterval(interval time.Duration) {
	_AnimeCheckInterval = interval
}

func CheckTrackedAnime(ctx *BotContext) {
	type NotificationSetting struct {
		guildID     snowflake.ID
		lastChecked int64
	}

	rows, err := ctx.DB.Query(`
		SELECT
			n.guild_id,
			n.last_checked
		FROM guild_anime_notifications n
		JOIN guilds g
			ON g.guild_id = n.guild_id
		WHERE (g.features & ?) != 0
	`, FeatureAnime)
	if err != nil {
		slog.Error(
			"failed to query anime notification settings",
			slog.Any("err", err),
		)
		return
	}
	defer rows.Close()

	var settings []NotificationSetting

	for rows.Next() {
		var setting NotificationSetting

		if err := rows.Scan(
			&setting.guildID,
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
		return
	}

	now := time.Now().Unix()
	client := &http.Client{}

	for _, setting := range settings {
		if time.Since(time.Unix(setting.lastChecked, 0)) < _AnimeCheckInterval {
			continue
		}

		checkGuildAnime(
			ctx,
			client,
			setting.guildID,
			now,
		)

		_, err = ctx.DB.Exec(`
			UPDATE guild_anime_notifications
			SET last_checked = ?
			WHERE guild_id = ?
		`,
			now,
			setting.guildID,
		)

		if err != nil {
			slog.Error(
				"failed to update anime notification check time",
				slog.Any("err", err),
				slog.String(
					"guild_id",
					setting.guildID.String(),
				),
			)
		}
	}
}

//-----------
// Private functions

type DescriptionBuilder struct {
	value     strings.Builder
	lineCount int
	truncated bool
}

func (b *DescriptionBuilder) AddLine(line string) bool {
	const maxLength = 4096

	length := b.value.Len()

	if length > 0 {
		length++
	}

	if length+len(line) > maxLength {
		b.truncated = true
		return false
	}

	if b.value.Len() > 0 {
		b.value.WriteByte('\n')
	}

	b.value.WriteString(line)
	b.lineCount++

	return true
}

func (b *DescriptionBuilder) String() string {
	return b.value.String()
}

func (b *DescriptionBuilder) Len() int {
	return b.lineCount
}

func (b *DescriptionBuilder) Truncated() bool {
	return b.truncated
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

func formatCountdown(target time.Time) string {
	duration := time.Until(target)

	if duration <= 0 {
		return "Airing now"
	}

	days := int(duration.Hours()) / 24
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf(
			"in %dd %dh %dm",
			days,
			hours,
			minutes,
		)
	}

	if hours > 0 {
		return fmt.Sprintf(
			"in %dh %dm",
			hours,
			minutes,
		)
	}

	return fmt.Sprintf(
		"in %dm",
		minutes,
	)
}

type UpcomingAnime struct {
	Anime    *Media
	Episode  int
	AiringAt time.Time
}

func getUpcomingAnime(
	ctx *BotContext,
	guildID snowflake.ID,
	userID snowflake.ID,
) ([]UpcomingAnime, string, error) {
	var timezone string

	err := ctx.DB.QueryRow(`
		SELECT timezone
		FROM user_timezones
		WHERE user_id = ?
	`, userID.String()).Scan(&timezone)

	if err == sql.ErrNoRows {
		timezone = "UTC"
	} else if err != nil {
		return nil, "", err
	}

	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, "", err
	}

	animeList, err := getTrackedAnime(ctx, guildID)
	if err != nil {
		return nil, "", err
	}

	var upcoming []UpcomingAnime

	for _, anime := range animeList {
		if anime.NextAiringEpisode == nil {
			continue
		}

		upcoming = append(
			upcoming,
			UpcomingAnime{
				Anime:   anime,
				Episode: anime.NextAiringEpisode.Episode,
				AiringAt: time.Unix(
					anime.NextAiringEpisode.AiringAt,
					0,
				).In(location),
			},
		)
	}

	sort.Slice(
		upcoming,
		func(i, j int) bool {
			return upcoming[i].AiringAt.Before(
				upcoming[j].AiringAt,
			)
		},
	)

	return upcoming, timezone, nil
}

func getAnimeTitle(anime *Media) string {
	if anime.Title.English != "" {
		return anime.Title.English
	}

	return anime.Title.Romaji
}

func getAnimeMinimalTitle(anime *MediaMinimal) string {
	if anime.Title.English != "" {
		return anime.Title.English
	}

	return anime.Title.Romaji
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

func enableAnimeNotifications(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
) {
	_, err := ctx.DB.Exec(`
		INSERT INTO guild_anime_notifications (
			guild_id,
			last_checked
		)
		VALUES (?, 0)
		ON CONFLICT(guild_id)
		DO NOTHING
	`,
		event.GuildID(),
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
			SendMessage(ctx, event)
		return
	}

	MessageBuild().
		Embed(
			discord.NewEmbed().
				WithTitle("Anime notifications enabled").
				WithDescription(
					fmt.Sprintf(
						"Anime episodes will be checked every `%s`.",
						_AnimeCheckInterval,
					),
				).
				WithColor(ColorSuccess),
		).
		SendMessage(ctx, event)
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
			SendMessage(ctx, event)
		return
	}

	MessageBuild().
		Embed(
			discord.NewEmbed().
				WithTitle("Anime notifications disabled").
				WithColor(ColorWarning),
		).
		SendMessage(ctx, event)
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

var ErrAnimePermissionDenied = errors.New(
	"You need the `anime` role to track anime.",
)

func setAnimeTracked(
	ctx *BotContext,
	member *discord.Member,
	guildID snowflake.ID,
	channelID snowflake.ID,
	anime *Media,
	track bool,
) error {
	if !HasRole(
		ctx,
		member,
		guildID,
		"anime",
	) {
		return ErrAnimePermissionDenied
	}

	if track {
		lastNotifiedEpisode := 0

		if anime.NextAiringEpisode != nil {
			lastNotifiedEpisode =
				anime.NextAiringEpisode.Episode - 1
		}

		_, err := ctx.DB.Exec(`
			INSERT OR IGNORE INTO tracked_anime (
				guild_id,
				anime_id,
				channel_id,
				last_notified_episode,
				last_updated_time
			)
			VALUES (?, ?, ?, ?, ?)
		`,
			guildID,
			anime.Id,
			channelID,
			lastNotifiedEpisode,
			time.Now().Unix(),
		)

		return err
	}

	_, err := ctx.DB.Exec(`
		DELETE FROM tracked_anime
		WHERE guild_id = ?
		AND anime_id = ?
	`,
		guildID,
		anime.Id,
	)

	return err
}

func handleTrackAnimeEx(
	ctx *BotContext,
	id int,
	member *discord.Member,
	guildID snowflake.ID,
	channelID snowflake.ID,
	track bool,
	send func(*MessageBuilder),
) {
	anime, err := GetAnime(&http.Client{}, id)
	if err != nil {
		slog.Error(
			"failed to get anime",
			slog.Any("err", err),
			slog.Int("anime_id", id),
		)

		send(
			MessageBuild().
				Embed(
					discord.NewEmbed().
						WithTitle("Failed to get anime").
						WithDescription(
							"An error occurred while getting the anime.",
						).
						WithColor(ColorError),
				),
		)
		return
	}

	err = setAnimeTracked(
		ctx,
		member,
		guildID,
		channelID,
		anime,
		track,
	)

	if err != nil {
		if errors.Is(err, ErrAnimePermissionDenied) {
			send(
				MessageBuild().
					Embed(
						discord.NewEmbed().
							WithTitle("Permission denied").
							WithDescription(err.Error()).
							WithThumbnail(anime.CoverImage.Medium).
							WithColor(ColorError),
					),
			)
			return
		}

		slog.Error(
			"failed to update tracked anime",
			slog.Any("err", err),
			slog.Int("anime_id", id),
		)

		send(
			MessageBuild().
				Embed(
					discord.NewEmbed().
						WithTitle("Failed to update anime").
						WithDescription(
							"An error occurred while updating tracked anime.",
						).
						WithColor(ColorError),
				),
		)
		return
	}

	title := getAnimeTitle(anime)

	if track {
		send(
			MessageBuild().
				Embed(
					discord.NewEmbed().
						WithTitle("Anime tracked").
						WithDescription(
							title + " is now being tracked.",
						).
						WithThumbnail(anime.CoverImage.Medium).
						WithColor(ColorSuccess),
				),
		)
		return
	}

	send(
		MessageBuild().
			Embed(
				discord.NewEmbed().
					WithTitle("Anime untracked").
					WithDescription(
						title + " is no longer being tracked.",
					).
					WithThumbnail(anime.CoverImage.Medium).
					WithColor(ColorWarning),
			),
	)
}

func handleTrackAnimeComponent(
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

	handleTrackAnimeEx(
		ctx,
		id,
		&event.Member().Member,
		*event.GuildID(),
		event.Message.ChannelID,
		track,
		func(message *MessageBuilder) {
			message.SendComponent(ctx, event)
		},
	)
}

func handleTrackAnime(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
	args map[string]string,
	track bool,
) {
	id, err := strconv.Atoi(args["name"])
	if err != nil {
		MessageBuild().
			Embed(
				discord.NewEmbed().
					WithTitle("Invalid anime").
					WithDescription("Could not find that anime.").
					WithColor(ColorError),
			).
			SendMessage(ctx, event)
		return
	}

	handleTrackAnimeEx(
		ctx,
		id,
		&event.Member().Member,
		*event.GuildID(),
		event.Channel().ID(),
		track,
		func(message *MessageBuilder) {
			message.SendMessage(ctx, event)
		},
	)
}

func getEpisodeTitle(
	anime *Media,
	episode int,
) string {
	index := episode - 1
	if index < 0 || index >= len(anime.StreamingEpisodes) {
		return ""
	}
	return anime.StreamingEpisodes[index].Title
}

func notifyAnimeEpisode(
	ctx *BotContext,
	channelID snowflake.ID,
	anime *Media,
	episode int,
) {
	episodeText := fmt.Sprintf(
		"Episode %d",
		episode,
	)

	episodeTitle := getEpisodeTitle(
		anime,
		episode,
	)

	if episodeTitle != "" {
		episodeText += " — " + episodeTitle
	}

	description := fmt.Sprintf(
		"**%s** is now available.",
		episodeText,
	)

	if anime.NextAiringEpisode != nil &&
		anime.NextAiringEpisode.Episode == episode+1 {
		description += fmt.Sprintf(
			"\n\nNext episode: <t:%d:R>",
			anime.NextAiringEpisode.AiringAt,
		)
	}

	embed := discord.NewEmbed().
		WithTitle("New episode released").
		WithURL(fmt.Sprintf("https://anilist.co/anime/%d", anime.Id)).
		WithDescription(description).
		WithThumbnail(anime.CoverImage.Medium).
		WithColor(getAnimeColor(anime)).
		WithFooter(
			getAnimeTitle(anime),
			"",
		)

	MessageBuild().
		Embed(embed).
		SendChannel(ctx, channelID)
}

func notifyAnimeFinished(
	ctx *BotContext,
	channelID snowflake.ID,
	anime *Media,
) {
	embed := discord.NewEmbed().
		WithTitle("Anime finished").
		WithURL(fmt.Sprintf("https://anilist.co/anime/%d", anime.Id)).
		WithDescription(
			fmt.Sprintf(
				"**%s** has finished airing.",
				getAnimeTitle(anime),
			),
		).
		WithThumbnail(anime.CoverImage.Medium).
		WithColor(getAnimeColor(anime))

	MessageBuild().
		Embed(embed).
		SendChannel(ctx, channelID)
}

func checkGuildAnime(
	ctx *BotContext,
	client *http.Client,
	guildID snowflake.ID,
	now int64,
) {
	type TrackedAnime struct {
		animeID             int
		channelID           snowflake.ID
		lastNotifiedEpisode int
		lastUpdatedTime     int64
	}

	rows, err := ctx.DB.Query(`
		SELECT
			anime_id,
			channel_id,
			last_notified_episode,
			last_updated_time
		FROM tracked_anime
		WHERE guild_id = ?
	`, guildID)

	if err != nil {
		slog.Error(
			"failed to query tracked anime",
			slog.Any("err", err),
			slog.String("guild_id", guildID.String()),
		)
		return
	}

	var trackedAnime []TrackedAnime

	for rows.Next() {
		var anime TrackedAnime

		if err := rows.Scan(
			&anime.animeID,
			&anime.channelID,
			&anime.lastNotifiedEpisode,
			&anime.lastUpdatedTime,
		); err != nil {
			slog.Error(
				"failed to scan tracked anime",
				slog.Any("err", err),
				slog.String("guild_id", guildID.String()),
			)
			continue
		}

		trackedAnime = append(
			trackedAnime,
			anime,
		)
	}

	if err := rows.Err(); err != nil {
		slog.Error(
			"failed while reading tracked anime",
			slog.Any("err", err),
			slog.String("guild_id", guildID.String()),
		)
	}

	rows.Close()

	// The SELECT above is finished and closed before any writes.
	for _, tracked := range trackedAnime {
		processTrackedAnime(
			ctx,
			client,
			guildID,
			now,
			tracked.animeID,
			tracked.channelID,
			tracked.lastNotifiedEpisode,
			tracked.lastUpdatedTime,
		)
	}
}

func processTrackedAnime(
	ctx *BotContext,
	client *http.Client,
	guildID snowflake.ID,
	now int64,
	animeID int,
	channelID snowflake.ID,
	lastNotifiedEpisode int,
	lastUpdatedTime int64,
) {
	anime, err := GetAnime(client, animeID)
	if err != nil {
		slog.Error(
			"failed to get anime",
			slog.Any("err", err),
			slog.Int("anime_id", animeID),
		)
		return
	}

	// Finished anime
	if anime.Status == "FINISHED" {
		if lastNotifiedEpisode < anime.Episodes {
			notifyAnimeFinished(
				ctx,
				channelID,
				anime,
			)

			_, err = ctx.DB.Exec(`
				UPDATE tracked_anime
				SET
					last_notified_episode = ?,
					last_updated_time = ?
				WHERE guild_id = ?
				AND anime_id = ?
			`,
				anime.Episodes,
				now,
				guildID,
				animeID,
			)

			if err != nil {
				slog.Error(
					"failed to update finished anime",
					slog.Any("err", err),
					slog.String(
						"guild_id",
						guildID.String(),
					),
					slog.Int("anime_id", animeID),
				)
			}

			return
		}

		// Remove finished anime one day after the finished
		// notification was sent.
		if lastUpdatedTime != 0 &&
			now-lastUpdatedTime >= 24*60*60 {

			_, err = ctx.DB.Exec(`
				DELETE FROM tracked_anime
				WHERE guild_id = ?
				AND anime_id = ?
			`,
				guildID,
				animeID,
			)

			if err != nil {
				slog.Error(
					"failed to remove finished anime",
					slog.Any("err", err),
					slog.String(
						"guild_id",
						guildID.String(),
					),
					slog.Int("anime_id", animeID),
				)
			}
		}

		return
	}

	// No upcoming episode.
	if anime.NextAiringEpisode == nil {
		return
	}

	airedEpisode := anime.NextAiringEpisode.Episode - 1

	// Nothing new since the last check.
	if airedEpisode <= lastNotifiedEpisode {
		return
	}

	notifyAnimeEpisode(
		ctx,
		channelID,
		anime,
		airedEpisode,
	)

	_, err = ctx.DB.Exec(`
		UPDATE tracked_anime
		SET
			last_notified_episode = ?,
			last_updated_time = ?
		WHERE guild_id = ?
		AND anime_id = ?
	`,
		airedEpisode,
		now,
		guildID,
		animeID,
	)

	if err != nil {
		slog.Error(
			"failed to update tracked anime",
			slog.Any("err", err),
			slog.String(
				"guild_id",
				guildID.String(),
			),
			slog.Int("anime_id", animeID),
		)
	}
}
