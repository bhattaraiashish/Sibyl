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

### Deploy script

The repository includes `deploy.sh`, which builds and starts both the bot and dashboard:

```bash
./deploy.sh
```

The script builds:

```text
sibyl
sibyl-dashboard
```

and starts both processes.

## Configuration

Sibyl uses an `env.json` file for environment variables and secrets. Application configuration is stored in the SQLite database.

The default environment file is:

```text
env.json
```

The default database file is:

```text
sibyl.db
```

Both paths can be changed using command-line options.

### Environment file

Create an `env.json` file:

```json
{
    "SIBYL_DEBUG": "true",
    "SIBYL_PORT": "8080",
    "LOG_PATH": ".",
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

### Database configuration

Sibyl stores application configuration in its SQLite database.

The default configuration is created automatically when the database is initialized.

Currently available options include:

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

Configuration can be changed through the web dashboard. The bot periodically checks the database and applies configuration changes without requiring a restart.

## Usage

Start the bot with the default environment file and database:

```bash
./sibyl
```

Use custom files with:

```bash
./sibyl -env ./env.json -db ./sibyl.db
```

In debug mode, all commands are registered per guild instead of globally. This allows command changes to take effect immediately during development.

### Command-line options

```text
-env <path>

    Path to the environment file.
    Default: env.json

-db <path>

    Path to the SQLite database.
    Default: sibyl.db

-debug

    Enable debug mode. Commands are registered per guild.
```

For example:

```bash
./sibyl -env ./production.json -db ./data/sibyl.db -debug
```

The bot reads its environment variables from the specified environment file and initializes the SQLite database automatically when started.

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

Or run it directly during development:

```bash
go run ./cmd/sibyl-dashboard
```

The dashboard uses the same default files:

```text
env.json
sibyl.db
```

Custom paths can be specified using the same `-env` and `-db` options:

```bash
./sibyl-dashboard -env ./env.json -db ./sibyl.db
```

The dashboard port is configured using `SIBYL_PORT` in the environment file.

Discord OAuth2 login is configured using the Discord variables in the environment file.

## Running Both

The bot and dashboard are separate processes and share the same SQLite database.

The simplest way to start both is:

```bash
./deploy.sh
```

Alternatively, start them separately:

```bash
./sibyl
```

```bash
./sibyl-dashboard
```

When using a custom database, make sure both processes use the same database path:

```bash
./sibyl -db ./data/sibyl.db
```

```bash
./sibyl-dashboard -db ./data/sibyl.db
```

The SQLite database contains application state and configuration, while the environment file contains environment-specific settings and secrets.
