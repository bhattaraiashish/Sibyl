package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/omit"
	"github.com/disgoorg/snowflake/v2"
)

type Timer struct {
	Interval time.Duration
	Handler  func(*BotContext)
}

type BotContext struct {
	DB              *sql.DB
	Commands        []Command
	Components      []Component
	Timers          []Timer
	FirstMessage    func(*BotContext)
	Client          *bot.Client
	HTTP            *http.Client
	MessageCount    atomic.Int64
	LastMessageTime atomic.Int64
	IsDebug         bool
}

// ----------
// Command system

type CommandHandler func(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
	args map[string]string,
)

type CommandPermission int

const (
	CommandPermissionNormal CommandPermission = iota
	CommandPermissionAdmin
)

type AutoCompleteChoice struct {
	Name  string
	Value string
}

type AutocompleteHandler func(
	ctx *BotContext,
	event *events.AutocompleteInteractionCreate,
	argument map[string]string,
) []AutoCompleteChoice

type CommandArgument struct {
	Name         string
	Description  string
	Required     bool
	Autocomplete AutocompleteHandler
}

type Command struct {
	Name        string
	Description string
	Permission  CommandPermission
	Feature     int64
	Arguments   []CommandArgument
	SubCommands []Command
	Handler     CommandHandler
}

type CommandBuilder struct {
	ctx     *BotContext
	command Command
}

func CommandBuild(
	ctx *BotContext,
	name string,
	description string,
) *CommandBuilder {
	return &CommandBuilder{
		ctx: ctx,
		command: Command{
			Name:        name,
			Description: description,
		},
	}
}

func (b *CommandBuilder) Permission(
	permission CommandPermission,
) *CommandBuilder {
	b.command.Permission = permission
	return b
}

func (b *CommandBuilder) Feature(
	feature int64,
) *CommandBuilder {
	b.command.Feature = feature
	return b
}

func (b *CommandBuilder) Argument(
	name string,
	description string,
	required bool,
) *CommandBuilder {
	b.command.Arguments = append(
		b.command.Arguments,
		CommandArgument{
			Name:        name,
			Description: description,
			Required:    required,
		},
	)
	return b
}

func (b *CommandBuilder) Autocomplete(
	handler AutocompleteHandler,
) *CommandBuilder {
	b.command.Arguments[len(b.command.Arguments)-1].Autocomplete = handler
	return b
}

func (b *CommandBuilder) Handler(
	handler CommandHandler,
) *CommandBuilder {
	b.command.Handler = handler
	return b
}

func (b *CommandBuilder) SubCommand(
	command *CommandBuilder,
) *CommandBuilder {
	b.command.SubCommands = append(
		b.command.SubCommands,
		command.command,
	)
	return b
}

func (b *CommandBuilder) Register() {
	b.ctx.Register(b.command)
}

func (command Command) Create() discord.SlashCommandCreate {
	options := make(
		[]discord.ApplicationCommandOption,
		0,
		len(command.Arguments)+len(command.SubCommands),
	)

	for _, argument := range command.Arguments {
		options = append(
			options,
			discord.ApplicationCommandOptionString{
				Name:         argument.Name,
				Description:  argument.Description,
				Required:     argument.Required,
				Autocomplete: argument.Autocomplete != nil,
			},
		)
	}

	for _, subCommand := range command.SubCommands {
		if len(subCommand.SubCommands) > 0 {
			subOptions := make(
				[]discord.ApplicationCommandOptionSubCommand,
				0,
				len(subCommand.SubCommands),
			)

			for _, nestedCommand := range subCommand.SubCommands {
				nestedOptions := make(
					[]discord.ApplicationCommandOption,
					0,
					len(nestedCommand.Arguments),
				)

				for _, argument := range nestedCommand.Arguments {
					nestedOptions = append(
						nestedOptions,
						discord.ApplicationCommandOptionString{
							Name:         argument.Name,
							Description:  argument.Description,
							Required:     argument.Required,
							Autocomplete: argument.Autocomplete != nil,
						},
					)
				}

				subOptions = append(
					subOptions,
					discord.ApplicationCommandOptionSubCommand{
						Name:        nestedCommand.Name,
						Description: nestedCommand.Description,
						Options:     nestedOptions,
					},
				)
			}

			options = append(
				options,
				discord.ApplicationCommandOptionSubCommandGroup{
					Name:        subCommand.Name,
					Description: subCommand.Description,
					Options:     subOptions,
				},
			)

			continue
		}

		subOptions := make(
			[]discord.ApplicationCommandOption,
			0,
			len(subCommand.Arguments),
		)

		for _, argument := range subCommand.Arguments {
			subOptions = append(
				subOptions,
				discord.ApplicationCommandOptionString{
					Name:         argument.Name,
					Description:  argument.Description,
					Required:     argument.Required,
					Autocomplete: argument.Autocomplete != nil,
				},
			)
		}

		options = append(
			options,
			discord.ApplicationCommandOptionSubCommand{
				Name:        subCommand.Name,
				Description: subCommand.Description,
				Options:     subOptions,
			},
		)
	}

	result := discord.SlashCommandCreate{
		Name:        command.Name,
		Description: command.Description,
		Options:     options,
	}

	if command.Permission == CommandPermissionAdmin {
		permissions := discord.Permissions(discord.PermissionAdministrator)
		result.DefaultMemberPermissions = omit.New(&permissions)
	}

	return result
}

type ComponentHandler func(
	ctx *BotContext,
	event *events.ComponentInteractionCreate,
	args []string,
)

type Component struct {
	ID      string
	Handler ComponentHandler
}

func (ctx *BotContext) RegisterComponent(
	id string,
	handler ComponentHandler,
) {
	ctx.Components = append(
		ctx.Components,
		Component{
			ID:      id,
			Handler: handler,
		},
	)
}

func ComponentID(id string, args ...string) string {
	if len(args) == 0 {
		return id
	}

	return id + ":" + strings.Join(args, ";")
}

func (ctx *BotContext) RegisterTimer(
	interval time.Duration,
	handler func(*BotContext),
) {
	ctx.Timers = append(
		ctx.Timers,
		Timer{
			Interval: interval,
			Handler:  handler,
		},
	)
}

func (ctx *BotContext) RegisterFirstMessage(
	handler func(*BotContext),
) {
	ctx.FirstMessage = handler
}

// ----------

func (ctx *BotContext) ResetMessageCount() {
	ctx.MessageCount.Store(0)
}

func (ctx *BotContext) SyncGuildCommands(
	guildID snowflake.ID,
) error {
	var features int64

	err := ctx.DB.QueryRow(`
		SELECT features
		FROM guilds
		WHERE guild_id = ?
	`, guildID).Scan(&features)
	if err != nil {
		return err
	}

	commands := make([]discord.ApplicationCommandCreate, 0)

	for _, command := range ctx.Commands {
		if command.Feature == 0 && !ctx.IsDebug {
			continue
		}

		if command.Feature != 0 &&
			features&command.Feature == 0 {
			continue
		}

		commands = append(commands, command.Create())
	}

	_, err = ctx.Client.Rest.SetGuildCommands(
		ctx.Client.ApplicationID,
		guildID,
		commands,
	)

	return err
}

func (ctx *BotContext) InitGuilds() error {
	guilds, err := ctx.Client.Rest.GetCurrentUserGuilds("", 0, 0, 0, false)
	if err != nil {
		return err
	}

	tx, err := ctx.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO guilds (guild_id)
		VALUES (?)
		ON CONFLICT(guild_id) DO NOTHING
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, guild := range guilds {
		if _, err := stmt.Exec(guild.ID); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	for _, guild := range guilds {
		if err := ctx.SyncGuildCommands(guild.ID); err != nil {
			return err
		}
	}

	return nil
}

func (ctx *BotContext) RegisterCommands() {
	if ctx.IsDebug {
		_, err := ctx.Client.Rest.SetGlobalCommands(
			ctx.Client.ApplicationID,
			[]discord.ApplicationCommandCreate{},
		)
		if err != nil {
			slog.Error(
				"failed to clear global commands",
				slog.Any("err", err),
			)
		}

		return
	}

	commands := make(
		[]discord.ApplicationCommandCreate,
		0,
	)

	for _, command := range ctx.Commands {
		if command.Feature != 0 {
			continue
		}

		commands = append(commands, command.Create())
	}

	_, err := ctx.Client.Rest.SetGlobalCommands(
		ctx.Client.ApplicationID,
		commands,
	)
	if err != nil {
		slog.Error(
			"failed to register global commands",
			slog.Any("err", err),
		)
	}
}

func (ctx *BotContext) StartTimers() {
	for _, timer := range ctx.Timers {
		go func(timer Timer) {
			ticker := time.NewTicker(timer.Interval)
			defer ticker.Stop()

			for {
				timer.Handler(ctx)
				<-ticker.C
			}
		}(timer)
	}
}

func (ctx *BotContext) Login(token string) error {
	slog.Info("debug mode", slog.Bool("is_debug", ctx.IsDebug))

	db, err := sql.Open("sqlite", "sibyl.db")
	if err != nil {
		return err
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return err
	}

	if err := initDatabase(db); err != nil {
		db.Close()
		return err
	}

	ctx.DB = db

	client, err := disgo.New(
		token,
		bot.WithGatewayConfigOpts(
			gateway.WithIntents(
				gateway.IntentsGuild|
					gateway.IntentMessageContent,
			),
		),
		bot.WithEventListenerFunc(func(event *events.Ready) {
			ctx.RegisterCommands()
			ctx.StartTimers()
			registerConnected(ctx, event)
		}),
		bot.WithEventListenerFunc(func(event *events.UserUpdate) {
			updateBotProfile(ctx, event)
		}),
		bot.WithEventListenerFunc(func(event *events.GuildJoin) {
			if err := ctx.SyncGuildCommands(event.Guild.ID); err != nil {
				slog.Error(
					"failed to sync guild commands",
					slog.Any("err", err),
					slog.String("guild_id", event.Guild.ID.String()),
				)
			}
		}),
		bot.WithEventListenerFunc(func(event *events.ApplicationCommandInteractionCreate) {
			ctx.Execute(event)
		}),
		bot.WithEventListenerFunc(func(event *events.AutocompleteInteractionCreate) {
			ctx.Autocomplete(event)
		}),
		bot.WithEventListenerFunc(func(event *events.ComponentInteractionCreate) {
			ctx.HandleComponentInteraction(event)
		}),
	)
	if err != nil {
		db.Close()
		return err
	}

	ctx.Client = client
	ctx.HTTP = &http.Client{}

	if err := ctx.InitGuilds(); err != nil {
		ctx.Client.Close(context.TODO())
		ctx.DB.Close()
		return err
	}

	if err := ctx.Client.OpenGateway(context.TODO()); err != nil {
		ctx.Client.Close(context.TODO())
		ctx.DB.Close()
		return err
	}

	startStatusHeartbeat(db, client)

	slog.Info("Sibyl is now running. Press CTRL-C to exit.")

	s := make(chan os.Signal, 1)
	signal.Notify(s, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	defer signal.Stop(s)

	<-s

	if ctx.IsDebug {
		ctx.UnregisterCommands()
	}

	ctx.Client.Close(context.TODO())
	ctx.DB.Close()
	return nil
}

func (ctx *BotContext) Register(command Command) {
	ctx.Commands = append(ctx.Commands, command)
}

func (ctx *BotContext) UnregisterCommands() {
	guilds, err := ctx.Client.Rest.GetCurrentUserGuilds("", 0, 0, 0, false)
	if err != nil {
		slog.Error(
			"failed to get guilds",
			slog.Any("err", err),
		)
		return
	}

	for _, guild := range guilds {
		_, err := ctx.Client.Rest.SetGuildCommands(
			ctx.Client.ApplicationID,
			guild.ID,
			[]discord.ApplicationCommandCreate{},
		)
		if err != nil {
			slog.Error(
				"failed to unregister commands",
				slog.Any("err", err),
				slog.String("guild_id", guild.ID.String()),
			)
		}
	}
}

func (ctx *BotContext) FindCommand(name string) *Command {
	for i := range ctx.Commands {
		if ctx.Commands[i].Name == name {
			return &ctx.Commands[i]
		}
	}
	return nil
}

func (ctx *BotContext) Execute(
	event *events.ApplicationCommandInteractionCreate,
) {
	if event.GuildID() == nil {
		return
	}

	data := event.SlashCommandInteractionData()

	command := ctx.FindCommand(data.CommandName())
	if command == nil {
		return
	}

	command = resolveCommand(command, data)
	if command == nil || command.Handler == nil {
		return
	}

	args := make(map[string]string)

	for name, option := range data.Options {
		var value string

		if err := json.Unmarshal(option.Value, &value); err != nil {
			slog.Error(
				"failed to unmarshal command option",
				slog.Any("err", err),
				slog.String("option", name),
				slog.String("value", string(option.Value)),
			)
			return
		}

		args[name] = value
	}

	command.Handler(ctx, event, args)
}

func (ctx *BotContext) Autocomplete(
	event *events.AutocompleteInteractionCreate,
) {
	data := event.Data

	command := ctx.FindCommand(data.CommandName)
	if command == nil {
		return
	}

	command = resolveAutocomplete(command, data)
	if command == nil {
		return
	}

	args := make(map[string]string)

	var focused string

	for name, option := range data.Options {
		var value string

		if err := json.Unmarshal(option.Value, &value); err != nil {
			slog.Error(
				"failed to unmarshal autocomplete option",
				slog.Any("err", err),
				slog.String("option", name),
				slog.String("value", string(option.Value)),
			)
			return
		}

		args[name] = value

		if option.Focused {
			focused = name
		}
	}

	for _, argument := range command.Arguments {
		if argument.Name != focused {
			continue
		}

		if argument.Autocomplete == nil {
			return
		}

		choices := argument.Autocomplete(
			ctx,
			event,
			args,
		)

		results := make(
			[]discord.AutocompleteChoice,
			0,
			len(choices),
		)

		for _, choice := range choices {
			results = append(
				results,
				discord.AutocompleteChoiceString{
					Name:  choice.Name,
					Value: choice.Value,
				},
			)
		}

		if err := event.AutocompleteResult(results); err != nil {
			slog.Error(
				"failed to send autocomplete result",
				slog.Any("err", err),
			)
		}

		return
	}
}

func (ctx *BotContext) HandleComponentInteraction(
	event *events.ComponentInteractionCreate,
) {
	parts := strings.Split(event.Data.CustomID(), ":")

	if len(parts) == 0 {
		return
	}

	id := parts[0]

	var args []string

	if len(parts) > 1 {
		args = strings.Split(parts[1], ";")
	}

	for _, component := range ctx.Components {
		if component.ID != id {
			continue
		}

		component.Handler(ctx, event, args)
		return
	}
}

const (
	ColorSuccess = 0x57F287
	ColorError   = 0xED4245
	ColorWarning = 0xF1C40F
	ColorInfo    = 0x3498DB
)

type MessageBuilder struct {
	discord.MessageCreate
}

func MessageBuild() *MessageBuilder {
	return &MessageBuilder{
		MessageCreate: discord.NewMessageCreate(),
	}
}

func (b *MessageBuilder) Embed(embed discord.Embed) *MessageBuilder {
	b.MessageCreate.Embeds = append(b.MessageCreate.Embeds, embed)
	return b
}

func (ctx *BotContext) UpdateMessageCount() {
	new := ctx.MessageCount.Add(1)
	ctx.LastMessageTime.Store(time.Now().Unix())
	if new == 1 && ctx.FirstMessage != nil {
		ctx.FirstMessage(ctx)
	}
}

func (b *MessageBuilder) SendChannel(
	ctx *BotContext,
	channelID snowflake.ID,
) {
	_, err := ctx.Client.Rest.CreateMessage(
		channelID,
		b.MessageCreate,
	)
	if err != nil {
		slog.Error(
			"failed to send message",
			slog.Any("err", err),
			slog.String("channel_id", channelID.String()),
		)
	}
	ctx.UpdateMessageCount()
}

func (b *MessageBuilder) SendMessage(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
) {
	err := event.CreateMessage(b.MessageCreate)

	if err != nil {
		slog.Error(
			"failed to send message",
			slog.Any("err", err),
		)
	}
	ctx.UpdateMessageCount()
}

func (b *MessageBuilder) SendComponent(
	ctx *BotContext,
	event *events.ComponentInteractionCreate,
) {
	err := event.CreateMessage(b.MessageCreate)

	if err != nil {
		slog.Error(
			"failed to send component message",
			slog.Any("err", err),
		)
	}
	ctx.UpdateMessageCount()
}

func (b *MessageBuilder) EditMessage(
	ctx *BotContext,
	event *events.ApplicationCommandInteractionCreate,
) {
	content := b.MessageCreate.Content
	embeds := b.MessageCreate.Embeds

	_, err := ctx.Client.Rest.UpdateInteractionResponse(
		ctx.Client.ApplicationID,
		event.Token(),
		discord.MessageUpdate{
			Content: &content,
			Embeds:  &embeds,
		},
	)
	if err != nil {
		slog.Error(
			"failed to edit interaction response",
			slog.Any("err", err),
		)
	}

	ctx.UpdateMessageCount()
}

func (ctx *BotContext) UpdatePresence(status string) {
	err := ctx.Client.SetPresence(
		context.Background(),
		gateway.WithCustomActivity(status),
	)

	if err != nil {
		slog.Error(
			"failed to update presence",
			slog.Any("err", err),
		)
	}
}

func HasRole(
	ctx *BotContext,
	member *discord.Member,
	guildID snowflake.ID,
	name string,
) bool {
	roles, err := ctx.Client.Rest.GetRoles(guildID)
	if err != nil {
		return false
	}

	for _, roleID := range member.RoleIDs {
		for _, role := range roles {
			if role.ID == roleID &&
				strings.EqualFold(role.Name, name) {
				return true
			}
		}
	}

	return false
}

// --------------------
// Private functions

func resolveCommand(
	command *Command,
	data discord.SlashCommandInteractionData,
) *Command {
	if data.SubCommandGroupName != nil {
		for i := range command.SubCommands {
			if command.SubCommands[i].Name != *data.SubCommandGroupName {
				continue
			}

			command = &command.SubCommands[i]
			break
		}
	}

	if data.SubCommandName != nil {
		for i := range command.SubCommands {
			if command.SubCommands[i].Name != *data.SubCommandName {
				continue
			}

			command = &command.SubCommands[i]
			break
		}
	}

	return command
}

func resolveAutocomplete(
	command *Command,
	data discord.AutocompleteInteractionData,
) *Command {
	if data.SubCommandGroupName != nil {
		for i := range command.SubCommands {
			if command.SubCommands[i].Name != *data.SubCommandGroupName {
				continue
			}

			command = &command.SubCommands[i]
			break
		}
	}

	if data.SubCommandName != nil {
		for i := range command.SubCommands {
			if command.SubCommands[i].Name != *data.SubCommandName {
				continue
			}

			command = &command.SubCommands[i]
			break
		}
	}

	return command
}

func registerConnected(ctx *BotContext, event *events.Ready) {
	now := time.Now().Unix()
	user := event.User
	latency := event.Client().Gateway.Latency().Milliseconds()

	_, err := ctx.DB.Exec(`
		INSERT INTO bot_status (
			id,
			user_id,
			username,
			avatar,
			connected_at,
			last_seen,
			latency
		)
		VALUES (1, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			user_id = excluded.user_id,
			username = excluded.username,
			avatar = excluded.avatar,
			connected_at = excluded.connected_at,
			last_seen = excluded.last_seen,
			latency = excluded.latency
	`,
		user.ID,
		user.Username,
		user.Avatar,
		now,
		now,
		latency,
	)

	if err != nil {
		slog.Error("failed to register bot connection", "error", err)
		return
	}
}

func updateBotProfile(ctx *BotContext, event *events.UserUpdate) {
	if event.User.ID != ctx.Client.ID() {
		return
	}

	_, err := ctx.DB.Exec(`
		UPDATE bot_status
		SET
			username = ?,
			avatar = ?
		WHERE id = 1
	`,
		event.User.Username,
		event.User.Avatar,
	)

	if err != nil {
		slog.Error("failed to update bot profile", "error", err)
	}
}

func startStatusHeartbeat(db *sql.DB, client *bot.Client) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			updateLastSeen(db, client)
		}
	}()
}

func updateLastSeen(db *sql.DB, client *bot.Client) {
	_, err := db.Exec(`
		UPDATE bot_status
		SET
			last_seen = ?,
			latency = ?
		WHERE id = 1
	`,
		time.Now().Unix(),
		client.Gateway.Latency().Milliseconds(),
	)

	if err != nil {
		slog.Error("failed to update bot status", "error", err)
	}
}

func initDatabase(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS guilds (
			guild_id TEXT PRIMARY KEY,
			features INTEGER NOT NULL DEFAULT 4294967295
		);

		CREATE TABLE IF NOT EXISTS user_timezones (
			user_id TEXT PRIMARY KEY,
			timezone TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS tracked_anime (
			guild_id TEXT NOT NULL,
			anime_id INTEGER NOT NULL,
			channel_id TEXT NOT NULL,
			last_notified_episode INTEGER NOT NULL DEFAULT 0,
			last_updated_time INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (guild_id, anime_id)
		);

		CREATE TABLE IF NOT EXISTS guild_anime_notifications (
			guild_id TEXT PRIMARY KEY,
			last_checked INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE IF NOT EXISTS rss_feeds (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			guild_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			url TEXT NOT NULL,
			last_item_id TEXT,
			last_checked INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE IF NOT EXISTS bot_status (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			user_id INTEGER NOT NULL DEFAULT 0,
			username TEXT NOT NULL DEFAULT '',
			avatar TEXT NOT NULL DEFAULT '',
			connected_at INTEGER NOT NULL DEFAULT 0,
			last_seen INTEGER NOT NULL DEFAULT 0,
			latency INTEGER NOT NULL DEFAULT 0
		);
	`)

	return err
}
