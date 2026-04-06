# immich-backup

A CLI tool for backing up your [Immich](https://immich.app/) media library and database using [rclone](https://rclone.org/). Ships as a single static binary — no runtime dependencies beyond rclone and Docker.

## Features

- **Media backup** — rclone sync to any storage backend (B2, S3, Google Drive, etc.)
- **Database backup** — `pg_dumpall` via `docker exec`, gzipped, no raw data directory copies
- **Interactive setup** — TUI wizard powered by Bubble Tea + Huh
- **Prerequisite checks** — `doctor` command verifies all dependencies before any backup
- **Daemon management** — install as a scheduled user service (no root required)
  - macOS: launchd user agent (`~/Library/LaunchAgents/`)
  - Linux: systemd user service (`~/.config/systemd/user/`)

## Requirements

- [rclone](https://rclone.org/install/) in `PATH`
- Docker socket accessible by your user (`docker` group on Linux)
- Immich stack running with its Postgres container

## Installation

### Homebrew (macOS / Linux)

```bash
# Coming soon
brew install daksh7011/tap/immich-backup
```

### Manual

Download the latest binary for your platform from the [releases page](https://github.com/daksh7011/immich-backup/releases) and place it somewhere on your `PATH`.

### Build from source

```bash
git clone https://github.com/daksh7011/immich-backup.git
cd immich-backup
CGO_ENABLED=0 go build -o immich-backup .
```

## Quick Start

```bash
# First-time setup: configures rclone remote + saves config
immich-backup setup

# Verify all prerequisites are met
immich-backup doctor

# Run a backup immediately
immich-backup backup
```

## Commands

| Command | Description |
|---------|-------------|
| `setup` | First-run wizard: configure rclone remote and backup settings |
| `configure` | Re-run the configuration wizard |
| `backup` | Run a full backup now (database + media) |
| `status` | Show last backup result and next scheduled run |
| `doctor` | Check all prerequisites and display results |
| `logs` | Show daemon log output |
| `daemon install` | Install and enable the background service |
| `daemon uninstall` | Remove the background service |
| `daemon start` | Start the background service |
| `daemon stop` | Stop the background service |
| `daemon restart` | Restart the background service |
| `daemon status` | Show background service status |
| `daemon logs` | Show background service logs |

## Configuration

Config lives at `~/.immich-backup/config.yaml`. Running `immich-backup setup` creates it with defaults.

```yaml
immich:
  upload_location: /mnt/immich       # Path to Immich upload directory
  postgres_container: immich_postgres # Docker container name
  postgres_user: postgres
  postgres_db: immich

backup:
  rclone_remote: "b2-encrypted:immich-backup"  # rclone remote:path
  schedule: "0 3 * * *"                         # Daily at 03:00
  db_backup_frequency: "0 */6 * * *"            # Every 6 hours
  retention:
    daily: 7
    weekly: 4

daemon:
  log_path: ~/.immich-backup/logs/daemon.log
```

### rclone configuration

`immich-backup` maintains its own isolated rclone config at `~/.immich-backup/rclone.conf` — it never touches your global rclone config. On first run, if no remote is configured, the tool pauses and launches `rclone config` interactively so you can add one.

## How it works

### Backup flow

1. **Doctor checks** — verifies rclone binary, rclone config has a remote, Docker socket, Postgres container running, config valid
2. **Database backup** — runs `pg_dumpall -U <user>` inside the Postgres container via `docker exec`, gzips the output to a temp file
3. **Media sync** — runs `rclone sync <upload_location> <remote> --config ~/.immich-backup/rclone.conf`
4. **Status write** — records result to `~/.immich-backup/last-run.json`

### Daemon scheduling

The daemon uses the OS scheduler directly:

- **macOS**: generates a `launchd` plist with `StartCalendarInterval` derived from the cron schedule and loads it with `launchctl`
- **Linux**: generates a `systemd` user unit file and enables it with `systemctl --user`

## Development

### Prerequisites

- Go 1.22+
- Docker socket accessible
- `rclone` in `PATH`

### Running tests

All tests run against real infrastructure — no mocks, no fakes.

```bash
CGO_ENABLED=0 go test ./... -timeout 300s
```

Tests that spin up containers (backup, docker, doctor) require Docker. They use [testcontainers-go](https://testcontainers.com/guides/getting-started-with-testcontainers-for-go/) and pull `postgres:17-alpine` and `alpine:latest`.

### Building

```bash
CGO_ENABLED=0 go build -o immich-backup .
```

Cross-compilation:

```bash
CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64  go build -o immich-backup-darwin-arm64  .
CGO_ENABLED=0 GOOS=linux   GOARCH=amd64  go build -o immich-backup-linux-amd64   .
CGO_ENABLED=0 GOOS=windows GOARCH=amd64  go build -o immich-backup-windows-amd64 .
```

## License

MIT
