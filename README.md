# Sibyl

Sibyl is a Discord bot written in Go with a web dashboard.

## Requirements

* Go 1.24 or newer
* A Discord bot application
* A Discord OAuth2 application
* SQLite

## Building

Clone the repository and build the bot:

```bash
go build -o sibyl ./cmd/sibyl
```

Build the dashboard:

```bash
go build -o sibyl-dashboard ./cmd/sibyl-dashboard
```

Or run either directly:

```bash
go run ./cmd/sibyl
```

```bash
go run ./cmd/sibyl-dashboard
```

## Configuration

Sibyl uses two separate JSON files:

* `config.json` — application settings
* `env.json` — environment variables and secrets

### Environment file

Create an `env.json` file:

```json
{
    "SIBYL_DEBUG": "true",
    "SIBYL_PORT": "8080",
    "DISCORD_BOT_TOKEN": "YOUR_DISCORD_BOT_TOKEN",
    "DISCORD_CLIENT_ID": "YOUR_DISCORD_CLIENT_ID",
    "DISCORD_CLIENT_SECRET": "YOUR_DISCORD_CLIENT_SECRET",
    "DISCORD_REDIRECT_URL": "http://localhost:8080/discord/callback"
}
```

`SIBYL_DEBUG` enables debug mode.

`SIBYL_PORT` specifies the port used by the dashboard.

`DISCORD_BOT_TOKEN` is the Discord bot token.

`DISCORD_CLIENT_ID` and `DISCORD_CLIENT_SECRET` are the Discord OAuth2 application credentials.

`DISCORD_REDIRECT_URL` is the OAuth2 callback URL registered with Discord.

### Configuration file

Create `config.json`:

```json
{
    "notification_interval": "30m"
}
```

### Options

`notification_interval`

The interval used for periodic notifications and feed checks.

It uses Go duration syntax:

```text
5m
30m
1h
2h30m
```

The default is `30m`.

The configuration file is automatically monitored while the bot is running. When `config.json` changes, Sibyl reloads the configuration and applies the new notification interval without restarting the bot.

## Usage

Start the bot with:

```bash
./sibyl -config ./config.json
```

In debug mode, all commands are registered per guild instead of globally. This allows command changes to take effect immediately during development.

### Command-line options

```text
-config <path>
    Path to the configuration file.

-debug
    Enable debug mode. Commands are registered per guild.
```

For example:

```bash
./sibyl -config ./config.json -debug
```

The bot reads its environment variables from `env.json` and initializes its database and Discord connection automatically when started.

## Dashboard

Sibyl includes a separate web dashboard.

Build it with:

```bash
go build -o sibyl-dashboard ./cmd/sibyl-dashboard
```

Run it with:

```bash
./sibyl-dashboard
```

The dashboard port is configured using `SIBYL_PORT` in `env.json`.

Or run it directly during development:

```bash
go run ./cmd/sibyl-dashboard
```

Discord OAuth2 login is configured using the Discord variables in `env.json`.

## Running Both

Run the bot and dashboard as separate processes:

```bash
./sibyl -config ./config.json
```

```bash
./sibyl-dashboard
```

The bot and dashboard share the same SQLite database, `config.json`, and `env.json`.
