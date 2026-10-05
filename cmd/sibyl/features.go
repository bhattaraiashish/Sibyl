package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
)

const FeatureAnime int64 = 1 << 0
const FeatureRSS int64 = 1 << 1

var Features = map[string]int64{
	"anime": FeatureAnime,
	"rss":   FeatureRSS,
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
			SendMessage(ctx, event)
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
		SendMessage(ctx, event)
}

func EnableFeatureCommand(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
	args map[string]string,
) {
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
			SendMessage(ctx, event)
		return
	}

	enableFeature(ctx, event, feature, name)
}

func DisableFeatureCommand(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
	args map[string]string,
) {
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
			SendMessage(ctx, event)
		return
	}

	disableFeature(ctx, event, feature, name)
}

func FeatureAutocomplete(
	ctx *BotContext,
	event *events.AutocompleteInteractionCreate,
	args map[string]string,
) []AutoCompleteChoice {
	value := strings.ToLower(args["feature"])

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
			}).SendMessage(ctx, event)
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
			}).SendMessage(ctx, event)
			return
		}

		MessageBuild().Embed(discord.Embed{
			Title:       "Timezone updated",
			Description: "Your timezone is now `" + timezone + "`.",
			Color:       ColorSuccess,
		}).SendMessage(ctx, event)
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
		}).SendMessage(ctx, event)
		return
	}

	if err != nil {
		slog.Error("failed to get timezone", slog.Any("err", err))
		MessageBuild().Embed(discord.Embed{
			Title:       "Database error",
			Description: "Failed to retrieve your timezone.",
			Color:       ColorError,
		}).SendMessage(ctx, event)
		return
	}

	MessageBuild().Embed(discord.Embed{
		Title:       "Your timezone",
		Description: "`" + timezone + "`",
		Color:       ColorSuccess,
	}).SendMessage(ctx, event)
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

func ClearChannelMessage(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
	args map[string]string,
) {
	channelID := event.Channel().ID()

	messages, err := ctx.Client.Rest.GetMessages(
		channelID,
		0, // around
		0, // before
		0, // after
		100,
	)
	if err != nil {
		slog.Error("failed to fetch channel messages", "error", err)

		MessageBuild().
			Embed(discord.Embed{
				Title:       "Clear failed",
				Description: "Failed to fetch channel messages.",
				Color:       ColorError,
			}).
			SendMessage(ctx, event)

		return
	}

	botID := ctx.Client.ID()

	var messageIDs []snowflake.ID

	for _, message := range messages {
		if message.Author.ID != botID {
			break
		}

		messageIDs = append(messageIDs, message.ID)
	}

	if len(messageIDs) == 0 {
		MessageBuild().
			Embed(discord.Embed{
				Title:       "Nothing to clear",
				Description: "There are no consecutive messages from the bot.",
				Color:       ColorError,
			}).
			SendMessage(ctx, event)

		return
	}

	if len(messageIDs) == 1 {
		if err := ctx.Client.Rest.DeleteMessage(
			channelID,
			messageIDs[0],
			nil,
		); err != nil {
			slog.Error(
				"failed to delete bot message",
				"message_id", messageIDs[0],
				"error", err,
			)

			MessageBuild().
				Embed(discord.Embed{
					Title:       "Clear failed",
					Description: "Failed to delete the bot message.",
					Color:       ColorError,
				}).
				SendMessage(ctx, event)

			return
		}
	} else {
		if err := ctx.Client.Rest.BulkDeleteMessages(
			channelID,
			messageIDs,
		); err != nil {
			slog.Error(
				"failed to bulk delete bot messages",
				"count", len(messageIDs),
				"error", err,
			)

			MessageBuild().
				Embed(discord.Embed{
					Title:       "Clear failed",
					Description: "Failed to delete the bot messages.",
					Color:       ColorError,
				}).
				SendMessage(ctx, event)

			return
		}
	}

	MessageBuild().
		Embed(discord.Embed{
			Title:       "Messages cleared",
			Description: fmt.Sprintf("Cleared %d message(s).", len(messageIDs)),
			Color:       ColorSuccess,
		}).
		SendMessage(ctx, event)
}

func InitCommonFeatures(ctx *BotContext) {
	CommandBuild(ctx, "feature", "Manage guild features").
		SubCommand(
			CommandBuild(ctx, "list", "List enabled features").
				Handler(ListFeaturesCommand),
		).
		SubCommand(
			CommandBuild(ctx, "enable", "Enable a feature").
				Argument("feature", "Feature to enable", true).
				Autocomplete(FeatureAutocomplete).
				Handler(EnableFeatureCommand),
		).
		SubCommand(
			CommandBuild(ctx, "disable", "Disable a feature").
				Argument("feature", "Feature to disable", true).
				Autocomplete(FeatureAutocomplete).
				Handler(DisableFeatureCommand),
		).
		Register()

	CommandBuild(ctx, "clear", "Clears upto last 100 bot messages").
		Handler(ClearChannelMessage).
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
			SendMessage(ctx, event)
		return
	}

	MessageBuild().
		Embed(
			discord.NewEmbed().
				WithTitle("Feature enabled").
				WithDescription("The "+name+" has been enabled.").
				WithColor(ColorSuccess),
		).
		SendMessage(ctx, event)

	syncGuildCommandsDelayed(ctx, *event.GuildID())
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
			SendMessage(ctx, event)
		return
	}

	MessageBuild().
		Embed(
			discord.NewEmbed().
				WithTitle("Feature disabled").
				WithDescription("The "+name+" has been disabled.").
				WithColor(ColorWarning),
		).
		SendMessage(ctx, event)

	syncGuildCommandsDelayed(ctx, *event.GuildID())
}

func syncGuildCommandsDelayed(
	ctx *BotContext,
	guildID snowflake.ID,
) {
	go func() {
		time.Sleep(5 * time.Second)

		if err := ctx.SyncGuildCommands(guildID); err != nil {
			slog.Error(
				"failed to sync guild commands",
				slog.Any("err", err),
				slog.String("guild_id", guildID.String()),
			)
		}
	}()
}
