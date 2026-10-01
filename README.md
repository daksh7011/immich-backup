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

- [rclone](https://rclone.org/install/) — on your `PATH`, or in a standard install directory (`~/.local/bin`, `~/bin`, `/opt/homebrew/bin`, `/usr/local/bin`, `/home/linuxbrew/.linuxbrew/bin`, `/usr/bin`, `/snap/bin`)
- Docker socket accessible by your user (`docker` group on Linux). For rootless Docker, Colima or Podman, set `DOCKER_HOST`.
- Immich stack running with its Postgres container
- For scheduled backups: systemd 240 or newer (Linux) or launchd (macOS)

## Installation

### Homebrew (macOS / Linux)

```bash
brew tap daksh7011/tap
brew install immich-backup
```

### Manual

Download the latest binary for your platform from the [releases page](https://github.com/daksh7011/immich-backup/releases) and place it somewhere on your `PATH`.

### Build from source

```bash
git clone https://github.com/daksh7011/immich-backup.git
cd immich-backup
CGO_ENABLED=0 go build -o immich-backup .
```

Install the binary somewhere permanent before running `daemon install`: the service runs the binary at the path it was installed from, and `daemon install` refuses a temporary build from `go run`.

## Quick Start

Run these as the user that should own the backups, **without sudo**:

```bash
# First-time setup: configures rclone remote + saves config
immich-backup setup

# Verify all prerequisites are met
immich-backup doctor

# Run a backup immediately
immich-backup backup

# Schedule the daily backup, then check it
immich-backup daemon install
immich-backup daemon status
```

`daemon install` only schedules backups; it does not run one. Use `immich-backup backup` for a run right now.

## Commands

| Command | Description |
|---------|-------------|
| `setup` | First-run wizard: configure rclone remote and backup settings |
| `configure` | Re-run the configuration wizard |
| `backup` | Run a full backup now (database + media); exits non-zero if any part fails |
| `status` | Show the last backup result, the last successful backup, and the next scheduled run |
| `doctor` | Check all prerequisites and the installed background service, and display results |
| `logs` | Show the scheduled runs' log (`--rclone` for rclone's debug log) |
| `daemon install` | Write the service files, enable the schedule, and (Linux) turn on lingering |
| `daemon uninstall` | Remove the background service |
| `daemon start` | Start the schedule (does not run a backup now) |
| `daemon stop` | Stop the schedule until the next `start`, login or reboot |
| `daemon restart` | Restart the schedule |
| `daemon status` | Show the schedule, next run and last scheduled run; exits non-zero when scheduled backups will not run |
| `daemon logs` | Show the scheduler's messages and the tail of the scheduled runs' log |

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
  schedule: "0 3 * * *"                         # Daily at 03:00 (MINUTE HOUR * * *)

daemon:
  log_path: ~/.immich-backup/logs/daemon.log   # must be absolute; a leading ~/ is expanded
```

`backup.schedule` is the time of the daily backup, written as `MINUTE HOUR * * *` with plain numbers (`30 2 * * *` runs at 02:30). Steps, ranges, lists, macros such as `@daily`, and day, month or weekday restrictions are rejected, because the daemon schedules one run per day. After changing the schedule, re-run `immich-backup daemon install` to apply it; `setup` and `configure` remind you when a service is installed.

`backup.db_backup_frequency` and `backup.retention` are **not yet implemented**: the database is dumped once per backup run and old dumps are not pruned. Existing configs that set them still load, but the values are ignored.

`daemon.log_path` defaults to `~/.immich-backup/logs/daemon.log` when omitted. A leading `~/` (in `log_path` and `upload_location`) is expanded to your home directory when the config is loaded; any other relative `log_path` is rejected. `daemon install` creates the log directory and file, since systemd and launchd will not start the job if it is missing. The installed service keeps the log path it was installed with, so re-run `daemon install` after changing it.

### rclone configuration

`immich-backup` maintains its own isolated rclone config at `~/.immich-backup/rclone.conf` — it never touches your global rclone config. On first run, if no remote is configured, the tool pauses and launches `rclone config` interactively so you can add one.

## Scheduled backups

`immich-backup daemon install` installs a per-user service that runs `immich-backup backup` daily at `backup.schedule`. It needs no root, and it must not be run with sudo: as root it would install into root's service manager, not yours. Run it again whenever you change the schedule or `log_path`, move the binary, or upgrade.

Scheduled runs do not get your shell's environment, so `daemon install` writes what they need into the unit or plist:

- the absolute path of the `immich-backup` binary. A Homebrew install uses the stable `bin` or `opt` link rather than the versioned Cellar path, so `brew upgrade` does not break the service.
- a `PATH` with rclone's directory first, then `/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin`. If rclone cannot be found, install fails.
- `DOCKER_HOST`, if it is set in the shell you run `daemon install` from. With rootless Docker, Colima or Podman, export it first.

Check the result with `immich-backup daemon status` (timer or job state, next run, last scheduled run and its result) and `immich-backup doctor`. `daemon logs` shows the scheduler's own messages, including failures that happen before the backup writes anything to its log.

### Linux (systemd)

`daemon install` writes `immich-backup.service` and `immich-backup.timer` to `~/.config/systemd/user/`, reloads systemd, enables and (re)starts the timer. The timer is `Persistent=true`, so a run missed while the machine was off runs as soon as the timer starts again (for example at the next boot).

**Lingering.** A systemd user timer runs only while your user manager is running. Without lingering, systemd stops it when your last login session ends and does not start it at boot, so on a headless server scheduled backups never run once you log out. `daemon install` checks this and runs `loginctl enable-linger <user>` when it is off. Many systems allow that only for root; then install fails after installing the timer and tells you to run:

```bash
sudo loginctl enable-linger "$USER"
```

Check it with `loginctl show-user "$USER" -p Linger`, or with `immich-backup doctor` (Daemon Linger) and `immich-backup daemon status`. Uninstalling the service leaves lingering on, since other user services may rely on it.

**Log in as the user directly** (for example `ssh user@host`), not through `sudo -u` or `su`: `systemctl --user` needs `XDG_RUNTIME_DIR`, and `daemon install`, `uninstall`, `start`, `stop` and `restart` fail if it is not set. If you have to use `su`, run `export XDG_RUNTIME_DIR=/run/user/$(id -u)` first.

`daemon start`, `stop` and `restart` act only on the timer. A backup that is already running is left to finish, and none of them starts a backup. `daemon logs` reads the journal for both units (`journalctl --user-unit=immich-backup.service --user-unit=immich-backup.timer`); errors such as `203/EXEC` (binary missing) or `209/STDOUT` (log path not writable) appear there, not in the log file.

### macOS (launchd)

`daemon install` writes `~/Library/LaunchAgents/com.immich-backup.agent.plist` and loads it into your GUI session with `launchctl bootstrap gui/<uid>`, then checks that it is loaded.

**GUI login required.** A LaunchAgent lives in a logged-in desktop session. Run the daemon commands from Terminal while logged in at the console or via Screen Sharing; over SSH alone they fail, because the session does not exist. Backups run only while you are logged in, so on a headless Mac enable automatic login. Running backups without a login would need a system LaunchDaemon, which requires root and is not supported.

**Full Disk Access.** If your Immich library is on an external drive or in a protected folder (Desktop, Documents, Downloads, network volumes), macOS blocks the scheduled job from reading it. Grant Full Disk Access to the `immich-backup` binary the service runs (`immich-backup doctor` shows the path) in System Settings → Privacy & Security → Full Disk Access. Re-grant it after every upgrade, since macOS ties the grant to that binary. A blocked or empty library makes the backup fail before syncing, instead of deleting the remote copy.

`daemon install`, `uninstall`, `stop` and `restart` unload the job, which interrupts a backup running at that moment. `daemon start` loads the job if it is not loaded and does not run a backup. launchd does not record run times, so `daemon status` shows the number of runs since the job was loaded and the last exit code.

### Upgrading

After upgrading immich-backup (`brew upgrade immich-backup`, a new release binary, or a rebuild), re-run:

```bash
immich-backup daemon install
immich-backup daemon status
```

This rewrites the unit or plist for the new version and binary path. `doctor` warns when the installed service points at a binary that no longer exists.

### Troubleshooting

- `immich-backup status` shows the result of the last backup, scheduled or manual, and when the last successful one ran. A failed or partial run does not hide the last success.
- `immich-backup daemon status` explains why scheduled backups are not running (not installed, timer stopped or disabled, lingering off, no GUI session, last run failed with its exit status) and exits non-zero when one of these applies.
- `immich-backup logs` shows the scheduled runs' log; `immich-backup logs --rclone` shows rclone's debug log (`~/.immich-backup/logs/rclone.log`).

## How it works

### Backup flow

1. **Doctor checks** — verifies rclone binary, rclone config has a remote, Docker socket, Postgres container running, config valid. A failure is recorded in the status file and the backup stops.
2. **Database backup** — runs `pg_dumpall -U <user>` inside the Postgres container via `docker exec`, gzips the output to a temp file, and uploads it to `<remote>/db/`
3. **Media sync** — checks that `upload_location` exists, is readable and is not empty (so an unmounted volume cannot wipe the remote), then runs `rclone sync <upload_location> <remote> --exclude /db/** --config ~/.immich-backup/rclone.conf`. The exclude keeps the sync from deleting the database dumps; it also means a top-level `db` folder inside `upload_location` is not synced.
4. **Status write** — records the result to `~/.immich-backup/last-run.json`: `success`, `partial` (rclone could not copy some files), or `error`

Any failure makes `backup` exit non-zero, so schedulers and scripts see it.

### Daemon scheduling

The daemon uses the OS scheduler directly:

- **macOS**: generates a `launchd` plist with `StartCalendarInterval` derived from the schedule and loads it with `launchctl bootstrap` into the `gui/<uid>` domain
- **Linux**: generates a `systemd` user service and timer (`OnCalendar` derived from the schedule) and enables the timer with `systemctl --user`

## Development

### Prerequisites

- Go 1.26+
- Docker socket accessible
- `rclone` in `PATH`

### Running tests

```bash
CGO_ENABLED=0 go test ./... -timeout 300s
```

Tests for the backup, docker and doctor packages run against real infrastructure. Those that spin up containers require Docker and rclone; they use [testcontainers-go](https://testcontainers.com/guides/getting-started-with-testcontainers-for-go/) and pull `postgres:17-alpine` and `alpine:latest`. The daemon tests never call systemctl, loginctl or launchctl: they run against a scripted command runner, so they pass on any OS.

### Building

```bash
CGO_ENABLED=0 go build -o immich-backup .
```

Cross-compilation:

```bash
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o immich-backup-darwin-arm64 .
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o immich-backup-darwin-amd64 .
CGO_ENABLED=0 GOOS=linux  GOARCH=arm64 go build -o immich-backup-linux-arm64  .
CGO_ENABLED=0 GOOS=linux  GOARCH=amd64 go build -o immich-backup-linux-amd64  .
```

## License

MIT
