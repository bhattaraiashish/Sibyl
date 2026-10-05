# Sibyl

Sibyl is a Discord bot written in Go with a web dashboard.

## Requirements

* Go 1.24 or newer
* A Discord bot application and token
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

Sibyl uses two separate files:

* `config.json` — application settings
* `token` — Discord bot token

Keeping the token separate prevents it from being stored in the JSON configuration or exposed through the dashboard.

### Discord token

Create a file named `token` containing only your Discord bot token:

```text
YOUR_DISCORD_BOT_TOKEN
```

The token file is read automatically when Sibyl starts. It is not specified through a command-line option.

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

Enable debug mode with:

```bash
./sibyl -config ./config.json -debug
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

The bot reads the Discord token from the `token` file and initializes its database and Discord connection automatically when started.

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

The dashboard listens on port `80` by default.

To use a different port:

```bash
./sibyl-dashboard -port 8080
```

Or run it directly during development:

```bash
go run ./cmd/sibyl-dashboard -port 8080
```

The dashboard uses the same `config.json` file as the bot. The Settings page loads the configuration file when the page is opened, so changes to the configuration are reflected without restarting the dashboard.

The Discord token is stored separately in the `token` file and is not part of the dashboard configuration.

## Running Both

Run the bot and dashboard as separate processes:

```bash
./sibyl -config ./config.json
```

```bash
./sibyl-dashboard -port 8080
```

The bot and dashboard share the same SQLite database and configuration file.
