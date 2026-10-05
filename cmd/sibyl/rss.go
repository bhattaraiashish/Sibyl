package main

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
)

var _RSSCheckInterval = 15 * time.Minute

func SetRSSCheckInterval(interval time.Duration) {
	_RSSCheckInterval = interval
}

func InitRSSFeatures(ctx *BotContext) {
	CommandBuild(ctx, "rss", "Manage RSS feeds").
		SubCommand(
			CommandBuild(ctx, "add", "Add an RSS feed").
				Argument("url", "The RSS feed URL", true).
				Handler(RSSAdd),
		).
		SubCommand(
			CommandBuild(ctx, "remove", "Remove an RSS feed").
				Argument("id", "The RSS feed ID", true).
				Handler(RSSRemove),
		).
		SubCommand(
			CommandBuild(ctx, "list", "List RSS feeds").
				Handler(RSSList),
		).
		Feature(FeatureRSS).
		Register()

	ctx.RegisterTimer(
		time.Minute,
		CheckRSS,
	)
}

func RSSAdd(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
	args map[string]string,
) {
	if err := event.DeferCreateMessage(false); err != nil {
		slog.Error(
			"failed to defer interaction",
			"error", err,
		)
		return
	}

	url := args["url"]

	parser := gofeed.NewParser()

	feed, err := parser.ParseURL(url)
	if err != nil {
		slog.Error(
			"failed to parse RSS feed",
			"url", url,
			"error", err,
		)

		MessageBuild().
			Embed(discord.Embed{
				Title:       "Invalid RSS feed",
				Description: "I couldn't parse that URL as an RSS or Atom feed.",
				Color:       ColorError,
			}).
			EditMessage(ctx, event)

		return
	}

	if len(feed.Items) == 0 {
		MessageBuild().
			Embed(discord.Embed{
				Title:       "Empty RSS feed",
				Description: "The feed doesn't contain any items.",
				Color:       ColorError,
			}).
			EditMessage(ctx, event)

		return
	}

	guildID := event.GuildID()
	channelID := event.Channel().ID()

	lastItemID := getRSSItemID(feed.Items[0])

	result, err := ctx.DB.Exec(`
		INSERT INTO rss_feeds (
			guild_id,
			channel_id,
			url,
			last_item_id,
			last_checked
		)
		VALUES (?, ?, ?, ?, ?)
	`,
		guildID,
		channelID,
		url,
		lastItemID,
		time.Now().Unix(),
	)
	if err != nil {
		slog.Error(
			"failed to add RSS feed",
			"guild_id", guildID,
			"channel_id", channelID,
			"url", url,
			"error", err,
		)

		MessageBuild().
			Embed(discord.Embed{
				Title:       "Failed to add RSS feed",
				Description: "An error occurred while saving the RSS feed.",
				Color:       ColorError,
			}).
			EditMessage(ctx, event)

		return
	}

	id, err := result.LastInsertId()
	if err != nil {
		slog.Error(
			"failed to get RSS feed ID",
			"error", err,
		)

		MessageBuild().
			Embed(discord.Embed{
				Title:       "RSS feed added",
				Description: "The RSS feed was added successfully.",
				Color:       ColorSuccess,
			}).
			EditMessage(ctx, event)

		return
	}

	MessageBuild().
		Embed(discord.Embed{
			Title: "RSS feed added",
			Description: fmt.Sprintf(
				"Feed #%d has been added to this channel.\n\n**%s**",
				id,
				feed.Title,
			),
			Color: ColorSuccess,
		}).
		EditMessage(ctx, event)
}

func RSSRemove(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
	args map[string]string,
) {
	id := args["id"]

	guildID := event.GuildID()

	result, err := ctx.DB.Exec(`
		DELETE FROM rss_feeds
		WHERE id = ? AND guild_id = ?
	`,
		id,
		guildID,
	)
	if err != nil {
		slog.Error("failed to remove RSS feed",
			"guild_id", guildID,
			"id", id,
			"error", err,
		)

		MessageBuild().
			Embed(discord.Embed{
				Title:       "Failed to remove RSS feed",
				Description: "An error occurred while removing the RSS feed.",
				Color:       ColorError,
			}).
			SendMessage(ctx, event)

		return
	}

	rows, err := result.RowsAffected()
	if err != nil {
		slog.Error("failed to check removed RSS feed",
			"guild_id", guildID,
			"id", id,
			"error", err,
		)

		return
	}

	if rows == 0 {
		MessageBuild().
			Embed(discord.Embed{
				Title: "RSS feed not found",
				Description: fmt.Sprintf(
					"No RSS feed with ID `%s` exists in this server.",
					id,
				),
				Color: ColorError,
			}).
			SendMessage(ctx, event)

		return
	}

	MessageBuild().
		Embed(discord.Embed{
			Title: "RSS feed removed",
			Description: fmt.Sprintf(
				"RSS feed `%s` has been removed.",
				id,
			),
			Color: ColorWarning,
		}).
		SendMessage(ctx, event)
}

func RSSList(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
	args map[string]string,
) {
	guildID := event.GuildID()

	rows, err := ctx.DB.Query(`
		SELECT id, url
		FROM rss_feeds
		WHERE guild_id = ?
		ORDER BY id
	`, guildID)
	if err != nil {
		slog.Error("failed to query RSS feeds",
			"guild_id", guildID,
			"error", err,
		)

		MessageBuild().
			Embed(discord.Embed{
				Title:       "Failed to list RSS feeds",
				Description: "An error occurred while loading RSS feeds.",
				Color:       ColorError,
			}).
			SendMessage(ctx, event)

		return
	}
	defer rows.Close()

	var description strings.Builder

	for rows.Next() {
		var (
			id  int64
			url string
		)

		if err := rows.Scan(&id, &url); err != nil {
			slog.Error("failed to scan RSS feed",
				"guild_id", guildID,
				"error", err,
			)
			continue
		}

		fmt.Fprintf(
			&description,
			"**%d** — <%s>\n",
			id,
			url,
		)
	}

	if err := rows.Err(); err != nil {
		slog.Error("failed to iterate RSS feeds",
			"guild_id", guildID,
			"error", err,
		)
	}

	if description.Len() == 0 {
		description.WriteString("No RSS feeds are configured.")
	}

	MessageBuild().
		Embed(discord.Embed{
			Title:       "RSS feeds",
			Description: description.String(),
			Color:       ColorInfo,
		}).
		SendMessage(ctx, event)
}

func CheckRSS(ctx *BotContext) {
	rows, err := ctx.DB.Query(`
		SELECT
			r.id,
			r.guild_id,
			r.channel_id,
			r.url,
			r.last_item_id,
			r.last_checked
		FROM rss_feeds r
		JOIN guilds g
			ON g.guild_id = r.guild_id
		WHERE (g.features & ?) != 0
	`, FeatureRSS)
	if err != nil {
		slog.Error("failed to query RSS feeds", "error", err)
		return
	}
	defer rows.Close()

	type RSSFeed struct {
		ID          int64
		GuildID     snowflake.ID
		ChannelID   snowflake.ID
		URL         string
		LastItemID  string
		LastChecked int64
	}

	var feeds []RSSFeed

	for rows.Next() {
		var feed RSSFeed

		if err := rows.Scan(
			&feed.ID,
			&feed.GuildID,
			&feed.ChannelID,
			&feed.URL,
			&feed.LastItemID,
			&feed.LastChecked,
		); err != nil {
			slog.Error("failed to scan RSS feed",
				"error", err,
			)
			continue
		}

		feeds = append(feeds, feed)
	}

	if err := rows.Err(); err != nil {
		slog.Error("failed to iterate RSS feeds", "error", err)
		return
	}

	now := time.Now().Unix()

	for _, feed := range feeds {
		if now-feed.LastChecked < int64(_RSSCheckInterval/time.Second) {
			continue
		}

		checkRSSFeed(ctx, feed)
	}
}

func checkRSSFeed(ctx *BotContext, rssFeed struct {
	ID          int64
	GuildID     snowflake.ID
	ChannelID   snowflake.ID
	URL         string
	LastItemID  string
	LastChecked int64
}) {
	parser := gofeed.NewParser()

	feed, err := parser.ParseURL(rssFeed.URL)
	if err != nil {
		slog.Error("failed to fetch RSS feed",
			"id", rssFeed.ID,
			"url", rssFeed.URL,
			"error", err,
		)

		updateRSSLastChecked(ctx, rssFeed.ID)
		return
	}

	if len(feed.Items) == 0 {
		updateRSSLastChecked(ctx, rssFeed.ID)
		return
	}

	lastItemIndex := -1

	for i, item := range feed.Items {
		if getRSSItemID(item) == rssFeed.LastItemID {
			lastItemIndex = i
			break
		}
	}

	// If the previous item is no longer in the feed, don't send
	// the entire existing feed as new. Just establish a new baseline.
	if lastItemIndex == -1 {
		updateRSSItem(ctx, rssFeed.ID, getRSSItemID(feed.Items[0]))
		return
	}

	if lastItemIndex == 0 {
		updateRSSLastChecked(ctx, rssFeed.ID)
		return
	}

	// gofeed normally returns items newest-first.
	// Reverse the new portion so notifications are chronological.
	newItems := feed.Items[:lastItemIndex]

	for i := len(newItems) - 1; i >= 0; i-- {
		notifyRSSItem(
			ctx,
			rssFeed.ChannelID,
			feed.Title,
			newItems[i],
		)
	}

	updateRSSItem(
		ctx,
		rssFeed.ID,
		getRSSItemID(feed.Items[0]),
	)
}

func notifyRSSItem(
	ctx *BotContext,
	channelID snowflake.ID,
	feedTitle string,
	item *gofeed.Item,
) {
	description := item.Description

	if description == "" {
		description = item.Content
	}

	if len(description) > 4096 {
		description = description[:4093] + "..."
	}

	embed := discord.Embed{
		Title:       item.Title,
		URL:         item.Link,
		Description: description,
		Color:       ColorInfo,
	}

	if feedTitle != "" {
		embed.Footer = &discord.EmbedFooter{
			Text: feedTitle,
		}
	}

	MessageBuild().
		Embed(embed).
		SendChannel(ctx, channelID)
}

func updateRSSItem(ctx *BotContext, id int64, itemID string) {
	_, err := ctx.DB.Exec(`
		UPDATE rss_feeds
		SET last_item_id = ?, last_checked = ?
		WHERE id = ?
	`,
		itemID,
		time.Now().Unix(),
		id,
	)
	if err != nil {
		slog.Error("failed to update RSS feed",
			"id", id,
			"error", err,
		)
	}
}

func updateRSSLastChecked(ctx *BotContext, id int64) {
	_, err := ctx.DB.Exec(`
		UPDATE rss_feeds
		SET last_checked = ?
		WHERE id = ?
	`,
		time.Now().Unix(),
		id,
	)
	if err != nil {
		slog.Error("failed to update RSS feed check time",
			"id", id,
			"error", err,
		)
	}
}

func getRSSItemID(item *gofeed.Item) string {
	if item.GUID != "" {
		return item.GUID
	}

	if item.Link != "" {
		return item.Link
	}

	return item.Title
}
