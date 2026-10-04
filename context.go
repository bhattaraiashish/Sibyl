package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/omit"
	"github.com/disgoorg/snowflake/v2"
)

type BotContext struct {
	DB       *sql.DB
	Commands []Command
	Client   *bot.Client
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

type AutocompleteHandler func(
	ctx *BotContext,
	event *events.AutocompleteInteractionCreate,
	argument map[string]string,
) []string

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

func (b *CommandBuilder) Register() {
	b.ctx.Register(b.command)
}

func (command Command) Create() discord.SlashCommandCreate {
	options := make([]discord.ApplicationCommandOption, 0, len(command.Arguments))

	for _, argument := range command.Arguments {
		options = append(options, discord.ApplicationCommandOptionString{
			Name:         argument.Name,
			Description:  argument.Description,
			Required:     argument.Required,
			Autocomplete: argument.Autocomplete != nil,
		})
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

// ----------

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

func (ctx *BotContext) Login(token string) error {
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
	)
	if err != nil {
		db.Close()
		return err
	}

	ctx.Client = client

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

	slog.Info("Sibyl is now running. Press CTRL-C to exit.")

	s := make(chan os.Signal, 1)
	signal.Notify(s, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	defer signal.Stop(s)

	<-s

	ctx.Client.Close(context.TODO())
	ctx.DB.Close()
	return nil
}

func (ctx *BotContext) Register(command Command) {
	ctx.Commands = append(ctx.Commands, command)
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
	name := data.CommandName()

	command := ctx.FindCommand(name)
	if command == nil {
		return
	}

	args := make(map[string]string)

	for _, argument := range command.Arguments {
		value := data.String(argument.Name)

		if value == "" && argument.Required {
			SendError(
				event,
				"Invalid arguments",
				"Missing required argument `"+argument.Name+"`.",
			)
			return
		}

		args[argument.Name] = value
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

		choices := argument.Autocomplete(ctx, event, args)

		results := make([]discord.AutocompleteChoice, 0, len(choices))

		for _, choice := range choices {
			results = append(
				results,
				discord.AutocompleteChoiceString{
					Name:  choice,
					Value: choice,
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

const (
	ColorSuccess = 0x57F287
	ColorError   = 0xED4245
)

func SendMessage(
	event *events.ApplicationCommandInteractionCreate,
	title string,
	description string,
	color int,
) {
	err := event.CreateMessage(
		discord.NewMessageCreate().
			WithEmbeds(
				discord.NewEmbed().
					WithTitle(title).
					WithDescription(description).
					WithColor(color),
			),
	)

	if err != nil {
		slog.Error(
			"failed to send message",
			slog.Any("err", err),
		)
	}
}

func SendSuccess(event *events.ApplicationCommandInteractionCreate, title, description string) {
	SendMessage(event, title, description, ColorSuccess)
}

func SendError(event *events.ApplicationCommandInteractionCreate, title, description string) {
	SendMessage(event, title, description, ColorError)
}

//--------------------
// Private functions

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
	`)

	return err
}
