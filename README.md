# Sibyl

Sibyl is a Discord bot written in Go.

## Requirements

* Go 1.24 or newer
* A Discord bot application and token
* SQLite

## Building

Clone the repository and build the bot:

```bash
go build -o sibyl .
```

Or run it directly:

```bash
go run .
```

## Configuration

Sibyl uses a JSON configuration file.

Create `config.json`:

```json
{
    "token": "YOUR_DISCORD_BOT_TOKEN",
    "notification_interval": "30m"
}
```

### Options

`token`

Your Discord bot token.

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

## Usage

Run Sibyl with:

```bash
./sibyl -config config.json
```

Enable debug mode with:

```bash
./sibyl -config config.json -debug
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

The bot will initialize its database and Discord connection automatically when started.
