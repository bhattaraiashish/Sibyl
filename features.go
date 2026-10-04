package main

import (
	"database/sql"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
)

const FeatureAnime int64 = 1 << 0

var Features = map[string]int64{
	"anime": FeatureAnime,
}

func ListFeaturesCommand(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
	args map[string]string,
) {
	var value int64

	err := ctx.DB.QueryRow(`
		SELECT features
		FROM guilds
		WHERE guild_id = ?
	`, event.GuildID()).Scan(&value)

	if err != nil {
		slog.Error(
			"failed to get guild features",
			slog.Any("err", err),
			slog.String("guild_id", event.GuildID().String()),
		)

		MessageBuild().
			Embed(
				discord.NewEmbed().
					WithTitle("Failed to get features").
					WithDescription(
						"An error occurred while getting guild features.",
					).
					WithColor(ColorError),
			).
			SendMessage(event)
		return
	}

	var enabled []string
	var disabled []string

	for name, feature := range Features {
		if value&feature != 0 {
			enabled = append(enabled, name)
		} else {
			disabled = append(disabled, name)
		}
	}

	sort.Strings(enabled)
	sort.Strings(disabled)

	var description strings.Builder

	description.WriteString("**Enabled**\n")

	if len(enabled) == 0 {
		description.WriteString("None\n")
	} else {
		description.WriteString(
			strings.Join(enabled, "\n"),
		)
	}

	description.WriteString("\n\n**Disabled**\n")

	if len(disabled) == 0 {
		description.WriteString("None")
	} else {
		description.WriteString(
			strings.Join(disabled, "\n"),
		)
	}

	MessageBuild().
		Embed(
			discord.NewEmbed().
				WithTitle("Features").
				WithDescription(description.String()).
				WithColor(ColorSuccess),
		).
		SendMessage(event)
}

func FeatureCommand(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
	args map[string]string,
) {
	action := strings.ToLower(args["action"])

	if action == "list" {
		listFeatures(ctx, event)
		return
	}

	name := strings.ToLower(args["feature"])
	feature, ok := getFeature(name)

	if !ok || feature == 0 {
		MessageBuild().
			Embed(
				discord.NewEmbed().
					WithTitle("Invalid feature").
					WithDescription("Unknown feature.").
					WithColor(ColorError),
			).
			SendMessage(event)
		return
	}

	switch action {
	case "enable":
		enableFeature(ctx, event, feature, name)

	case "disable":
		disableFeature(ctx, event, feature, name)

	default:
		MessageBuild().
			Embed(
				discord.NewEmbed().
					WithTitle("Invalid action").
					WithDescription("Use `enable` or `disable`.").
					WithColor(ColorError),
			).
			SendMessage(event)
	}
}

func FeatureAutocomplete(
	ctx *BotContext,
	event *events.AutocompleteInteractionCreate,
	args map[string]string,
) []AutoCompleteChoice {
	action := strings.ToLower(args["action"])
	value := strings.ToLower(args["feature"])

	if action == "list" {
		return nil
	}

	var choices []AutoCompleteChoice

	for name := range Features {
		if value != "" &&
			!strings.Contains(name, value) {
			continue
		}

		choices = append(
			choices,
			AutoCompleteChoice{
				Name:  name,
				Value: name,
			},
		)
	}

	return choices
}

func TimezoneCommand(
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

func TimezoneAutocomplete(
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

func FeatureActionAutocomplete(
	ctx *BotContext,
	event *events.AutocompleteInteractionCreate,
	args map[string]string,
) []AutoCompleteChoice {
	value := strings.ToLower(args["action"])

	actions := []string{
		"list",
		"enable",
		"disable",
	}

	var choices []AutoCompleteChoice

	for _, action := range actions {
		if value != "" &&
			!strings.Contains(action, value) {
			continue
		}

		choices = append(
			choices,
			AutoCompleteChoice{
				Name:  action,
				Value: action,
			},
		)
	}

	return choices
}

func EnableDisableAutocomplete(
	ctx *BotContext,
	event *events.AutocompleteInteractionCreate,
	args map[string]string,
) []AutoCompleteChoice {
	value := strings.ToLower(args["action"])

	actions := []string{
		"enable",
		"disable",
	}

	var choices []AutoCompleteChoice

	for _, action := range actions {
		if value != "" &&
			!strings.Contains(action, value) {
			continue
		}

		choices = append(
			choices,
			AutoCompleteChoice{
				Name:  action,
				Value: action,
			},
		)
	}

	return choices
}

func InitCommonFeatures(ctx *BotContext) {
	CommandBuild(ctx, "feature", "Enable or disable a feature").
		Argument("action", "Enable or disable", true).
		Autocomplete(FeatureActionAutocomplete).
		Argument("feature", "Feature to configure", false).
		Autocomplete(FeatureAutocomplete).
		Handler(FeatureCommand).
		Register()

	CommandBuild(ctx, "timezone", "Set or show your timezone").
		Argument("timezone", "Your IANA timezone", false).
		Autocomplete(TimezoneAutocomplete).
		Handler(TimezoneCommand).
		Register()
}

//------------------
// Private functions

func getFeature(name string) (int64, bool) {
	feature, ok := Features[name]
	return feature, ok
}

func listFeatures(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
) {
	var value int64

	err := ctx.DB.QueryRow(`
		SELECT features
		FROM guilds
		WHERE guild_id = ?
	`, event.GuildID()).Scan(&value)

	if err != nil {
		slog.Error(
			"failed to get guild features",
			slog.Any("err", err),
			slog.String("guild_id", event.GuildID().String()),
		)

		MessageBuild().
			Embed(
				discord.NewEmbed().
					WithTitle("Failed to get features").
					WithDescription(
						"An error occurred while getting guild features.",
					).
					WithColor(ColorError),
			).
			SendMessage(event)
		return
	}

	var enabled []string
	var disabled []string

	for name, feature := range Features {
		if value&feature != 0 {
			enabled = append(enabled, "`"+name+"`")
		} else {
			disabled = append(disabled, "`"+name+"`")
		}
	}

	sort.Strings(enabled)
	sort.Strings(disabled)

	var sections []string

	if len(enabled) > 0 {
		sections = append(
			sections,
			"**Enabled**\n"+strings.Join(enabled, ", "),
		)
	}

	if len(disabled) > 0 {
		sections = append(
			sections,
			"**Disabled**\n"+strings.Join(disabled, ", "),
		)
	}

	MessageBuild().
		Embed(
			discord.NewEmbed().
				WithTitle("Features").
				WithDescription(
					strings.Join(sections, "\n\n"),
				).
				WithColor(ColorSuccess),
		).
		SendMessage(event)
}

func enableFeature(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
	feature int64,
	name string,
) {
	_, err := ctx.DB.Exec(`
		UPDATE guilds
		SET features = features | ?
		WHERE guild_id = ?
	`,
		feature,
		event.GuildID(),
	)

	if err != nil {
		slog.Error(
			"failed to enable feature",
			slog.Any("err", err),
			slog.String("guild_id", event.GuildID().String()),
		)

		MessageBuild().
			Embed(
				discord.NewEmbed().
					WithTitle("Failed to enable feature").
					WithDescription(
						"An error occurred while enabling the feature.",
					).
					WithColor(ColorError),
			).
			SendMessage(event)
		return
	}

	if err := ctx.SyncGuildCommands(*event.GuildID()); err != nil {
		slog.Error(
			"failed to sync guild commands",
			slog.Any("err", err),
			slog.String("guild_id", event.GuildID().String()),
		)
	}

	MessageBuild().
		Embed(
			discord.NewEmbed().
				WithTitle("Feature enabled").
				WithDescription("The " + name + " has been enabled.").
				WithColor(ColorSuccess),
		).
		SendMessage(event)
}

func disableFeature(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
	feature int64,
	name string,
) {
	_, err := ctx.DB.Exec(`
		UPDATE guilds
		SET features = features & ~?
		WHERE guild_id = ?
	`,
		feature,
		event.GuildID(),
	)

	if err != nil {
		slog.Error(
			"failed to disable feature",
			slog.Any("err", err),
			slog.String("guild_id", event.GuildID().String()),
		)

		MessageBuild().
			Embed(
				discord.NewEmbed().
					WithTitle("Failed to disable feature").
					WithDescription(
						"An error occurred while disabling the feature.",
					).
					WithColor(ColorError),
			).
			SendMessage(event)
		return
	}

	if err := ctx.SyncGuildCommands(*event.GuildID()); err != nil {
		slog.Error(
			"failed to sync guild commands",
			slog.Any("err", err),
			slog.String("guild_id", event.GuildID().String()),
		)
	}

	MessageBuild().
		Embed(
			discord.NewEmbed().
				WithTitle("Feature disabled").
				WithDescription("The " + name + " has been disabled.").
				WithColor(ColorWarning),
		).
		SendMessage(event)
}
