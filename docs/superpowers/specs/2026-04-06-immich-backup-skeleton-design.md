# Design Spec: immich-backup Go Skeleton

**Date**: 2026-04-06
**Status**: Approved
**Module**: `github.com/daksh7011/immich-backup`

## Overview

A Go CLI tool for backing up Immich (self-hosted photo server) data using rclone.
Distributed as a standalone binary via Homebrew (macOS/Linux) and Chocolatey (Windows).
Built with Cobra for CLI, Bubble Tea + Lip Gloss + Huh for TUI, and yaml.v3 for config.

## Constitution Compliance

All design decisions comply with the `immich-backup` constitution v1.1.0:

- No CGo. `CGO_ENABLED=0` throughout.
- Rclone remote name is the only coupling point to storage backends.
- Fail-fast before any backup if rclone binary, Docker socket, or Postgres container
  is unreachable.
- Database backup exclusively via `pg_dumpall` through `docker exec`.
- Rclone configuration is never created or modified by this tool.
- Daemon management via launchd (macOS) and systemd user service (Linux). No root required.
- All tests run against real infrastructure (testcontainers-go + real rclone binary).
  No mocks, no fakes, no build tags.

## Section 1: Directory Structure

```
immich-backup/
├── main.go                          # entry point: calls cmd.Execute()
├── go.mod                           # module github.com/daksh7011/immich-backup
├── go.sum
│
├── cmd/
│   ├── root.go                      # root command, global flags, config loading middleware
│   ├── setup.go                     # interactive first-run wizard (calls config.Load directly)
│   ├── configure.go                 # re-run any part of setup (calls config.Load directly)
│   ├── backup.go                    # run a backup now; runs full doctor.Check() internally
│   ├── status.go                    # last backup status + next scheduled run
│   ├── doctor.go                    # display all prerequisite check results
│   ├── logs.go                      # tail/show log file (reads log path from config directly)
│   └── daemon.go                    # sub-commands: install/uninstall/start/stop/restart/status/logs
│
├── internal/
│   ├── config/
│   │   ├── config.go                # Config struct + Load/Save/Validate using yaml.v3
│   │   └── config_test.go
│   ├── backup/
│   │   ├── backup.go                # orchestrates media sync + db dump; rclone exec lives here
│   │   └── backup_test.go
│   ├── docker/
│   │   ├── docker.go                # Docker socket client, exec, container lookup
│   │   └── docker_test.go
│   ├── daemon/
│   │   ├── daemon.go                # install/uninstall/control launchd & systemd
│   │   ├── launchd.go               # macOS plist generation
│   │   ├── systemd.go               # Linux unit file generation
│   │   └── daemon_test.go
│   ├── doctor/
│   │   ├── doctor.go                # prerequisite checks (rclone binary, docker socket, container)
│   │   └── doctor_test.go
│   ├── status/
│   │   ├── status.go                # read/write ~/.immich-backup/last-run.json
│   │   └── status_test.go
│   └── tui/
│       ├── model.go                 # shared Bubble Tea types/helpers
│       ├── setup_model.go           # Huh form model for setup wizard
│       ├── configure_model.go
│       ├── backup_model.go          # live progress display; receives msgs via channel
│       ├── status_model.go
│       ├── doctor_model.go
│       ├── logs_model.go
│       └── daemon_model.go
│
└── docs/
    └── superpowers/specs/
        └── 2026-04-06-immich-backup-skeleton-design.md
```

## Section 2: Key Interfaces & Data Flow

### Dependency direction

```
cmd/ → internal/tui/
cmd/ → internal/<domain>/
internal/<domain>/ → internal/config/
internal/<domain>/ → internal/docker/
```

`cmd/` is the only package that imports `internal/tui/`. Domain packages never import
`tui/` or `cmd/`. Dependencies flow inward only.

### PersistentPreRun responsibility

`root.go` registers a `PersistentPreRun` that runs for every command **except**
`setup` and `configure` (which load config themselves). Its sole responsibility is:

1. Call `config.Load("~/.immich-backup/config.yaml")` and store the result in the
   Cobra command context.
2. If `config.Load()` returns an error (validation failure or malformed file), print
   the error and exit with code 1 immediately.

`PersistentPreRun` does **not** call `doctor.Check()`. Doctor checks are the
responsibility of individual command handlers:

| Command    | Runs doctor check? | Notes |
|------------|--------------------|-------|
| `backup`   | Yes — full check   | Fails fast if any check fails |
| `doctor`   | Yes — full check   | Displays all results, does not exit on failure |
| `setup`    | No                 | Exempt; may run before prerequisites exist |
| `configure`| No                 | Exempt; may run before prerequisites exist |
| `status`   | No                 | Must work even when backup prerequisites are down |
| `logs`     | No                 | Must work even when backup prerequisites are down |
| `daemon`   | No                 | Service management must work independently of backup prereqs |

### Core interfaces and functions

```go
// internal/docker
type Executor interface {
    Exec(container, command string, args ...string) ([]byte, error)
    IsContainerRunning(name string) (bool, error)
}

// internal/backup
// rclone subprocess execution lives inside backup.go alongside RunMedia/RunDatabase.
// No separate rclone package — rclone is invoked via os/exec within this package only.
type Runner interface {
    RunMedia(remote, srcDir string) error
    RunDatabase(container, pgUser, pgDB, destPath string) error
}

// internal/doctor
type CheckResult struct {
    Name    string
    OK      bool
    Message string
    Remedy  string
}
// Check verifies: (1) rclone binary in PATH via exec.LookPath,
// (2) Docker socket accessible, (3) Postgres container running, (4) config valid.
func Check(exec docker.Executor, cfg *config.Config) []CheckResult

// internal/daemon
type Manager interface {
    Install(cfg *config.Config) error
    Uninstall() error
    Start() error
    Stop() error
    Restart() error
    Status() (string, error)
    Logs() (string, error)
}
// New returns the platform-appropriate Manager (launchd on macOS, systemd on Linux).
func New() Manager

// internal/status
type LastRun struct {
    Time   time.Time `json:"time"`
    Result string    `json:"result"`  // "success" | "error"
    Error  string    `json:"error,omitempty"`
}
func Load(path string) (*LastRun, error)
func Save(path string, r *LastRun) error
```

### Backup data flow (live TUI progress)

`cmd/backup.go` starts a Bubble Tea program first, then the backup runner sends
progress messages through a channel that the TUI model consumes via `tea.Cmd`:

```
cmd/backup.go
  → doctor.Check()                    // fail-fast: exit 1 if any check fails
  → ch := make(chan tea.Msg)
  → go backup.Run(cfg, exec, ch)      // RunDatabase(container, pgUser, pgDB) then RunMedia; sends progress msgs
  → tea.NewProgram(backup_model{ch})  // blocks until backup completes
      ↳ backup_model.Update()         // receives ProgressMsg, ErrorMsg, DoneMsg from ch
  → status.Save()                     // write ~/.immich-backup/last-run.json after program exits
```

The backup runner sends typed messages (`ProgressMsg`, `ErrorMsg`, `DoneMsg`) to the
channel. The Bubble Tea model's `Update()` function reads from the channel via a
`tea.Cmd` that wraps a channel receive. This keeps `internal/backup` free of any TUI
imports — it only writes to a plain `chan tea.Msg` received as a parameter.

### Config flow

```
PersistentPreRun (all commands except setup and configure):
  → config.Load("~/.immich-backup/config.yaml")
  → stored in cobra.Command context
  → passed to each sub-command handler

setup and configure:
  → call config.Load() directly at the start of their Run function
  → config.Load() auto-creates the file with defaults if missing
  → after Huh form completes, call config.Save() to persist user choices
```

### Logs command

`cmd/logs.go` reads `Config.Daemon.LogPath` from the Cobra context and tails that
file directly using standard file I/O. No domain package needed.

### Status command

`cmd/status.go` calls `status.Load("~/.immich-backup/last-run.json")` for last-run
data and reads `Config.Backup.Schedule` to display the next scheduled run time.

## Section 3: Config Schema

### File location

`~/.immich-backup/config.yaml`

### Format

```yaml
immich:
  upload_location: /mnt/immich           # source directory for media sync
  postgres_container: immich_postgres     # docker container name
  postgres_user: postgres                 # postgres superuser for pg_dumpall
  postgres_db: immich                     # database name

backup:
  rclone_remote: "b2-encrypted:immich-backup"  # remote:path — no provider flags ever
  schedule: "0 3 * * *"                         # cron: media backup frequency
  db_backup_frequency: "0 */6 * * *"            # cron: database dump frequency
  retention:
    daily: 7                                     # keep last N daily backups
    weekly: 4                                    # keep last N weekly backups

daemon:
  log_path: ~/.immich-backup/logs/daemon.log
```

### Go structs

```go
type Config struct {
    Immich  ImmichConfig  `yaml:"immich"`
    Backup  BackupConfig  `yaml:"backup"`
    Daemon  DaemonConfig  `yaml:"daemon"`
}

type ImmichConfig struct {
    UploadLocation    string `yaml:"upload_location"`
    PostgresContainer string `yaml:"postgres_container"`
    PostgresUser      string `yaml:"postgres_user"`
    PostgresDB        string `yaml:"postgres_db"`
}

type BackupConfig struct {
    RcloneRemote      string          `yaml:"rclone_remote"`
    Schedule          string          `yaml:"schedule"`
    DBBackupFrequency string          `yaml:"db_backup_frequency"`
    Retention         RetentionConfig `yaml:"retention"`
}

type RetentionConfig struct {
    Daily  int `yaml:"daily"`
    Weekly int `yaml:"weekly"`
}

type DaemonConfig struct {
    LogPath string `yaml:"log_path"`
}
```

### Load-or-initialize behavior

If the config file does not exist, `Load()` writes the defaults to disk and returns
them. `setup` and `configure` call `Load()` directly, which triggers auto-creation
on first run without needing a separate init step.

```go
var defaults = Config{
    Immich: ImmichConfig{
        UploadLocation:    "/mnt/immich",
        PostgresContainer: "immich_postgres",
        PostgresUser:      "postgres",
        PostgresDB:        "immich",
    },
    Backup: BackupConfig{
        RcloneRemote:      "b2-encrypted:immich-backup",
        Schedule:          "0 3 * * *",
        DBBackupFrequency: "0 */6 * * *",
        Retention:         RetentionConfig{Daily: 7, Weekly: 4},
    },
    Daemon: DaemonConfig{
        LogPath: "~/.immich-backup/logs/daemon.log",
    },
}

func Load(path string) (*Config, error) {
    if _, err := os.Stat(path); os.IsNotExist(err) {
        cfg := defaults
        return &cfg, Save(path, &cfg)
    }
    // unmarshal then call Validate()
}
```

### Strict validation

`Load()` calls `Validate()` after every unmarshal. Any malformed or missing field
is a hard error — the caller exits with code 1 and a clear message listing every
failed field. The user can fix via `immich-backup configure` or by editing the file
manually. `doctor` surfaces these errors as a named `CheckResult` with a remediation
hint.

```go
func (c *Config) Validate() error {
    var errs []string
    if c.Immich.UploadLocation == ""    { errs = append(errs, "immich.upload_location is required") }
    if c.Immich.PostgresContainer == "" { errs = append(errs, "immich.postgres_container is required") }
    if c.Immich.PostgresUser == ""      { errs = append(errs, "immich.postgres_user is required") }
    if c.Immich.PostgresDB == ""        { errs = append(errs, "immich.postgres_db is required") }
    if c.Backup.RcloneRemote == ""      { errs = append(errs, "backup.rclone_remote is required") }
    if c.Backup.Schedule == ""          { errs = append(errs, "backup.schedule is required") }
    if !validCron(c.Backup.Schedule)    { errs = append(errs, "backup.schedule is not a valid cron expression") }
    if c.Backup.DBBackupFrequency == "" { errs = append(errs, "backup.db_backup_frequency is required") }
    if !validCron(c.Backup.DBBackupFrequency) { errs = append(errs, "backup.db_backup_frequency is not a valid cron expression") }
    if c.Backup.Retention.Daily <= 0   { errs = append(errs, "backup.retention.daily must be > 0") }
    if c.Backup.Retention.Weekly <= 0  { errs = append(errs, "backup.retention.weekly must be > 0") }
    if c.Daemon.LogPath == ""           { errs = append(errs, "daemon.log_path is required") }
    if len(errs) > 0 {
        return fmt.Errorf("config validation failed:\n  - %s", strings.Join(errs, "\n  - "))
    }
    return nil
}
```

Doctor check output example:

```
✗  Config       invalid: backup.schedule is not a valid cron expression
   → Run `immich-backup configure` to fix, or edit ~/.immich-backup/config.yaml manually
```

## Section 4: Error Handling & Logging

### Error handling strategy

- `PersistentPreRun` in `root.go` loads config and exits with code `1` on any config
  error. It does not run doctor checks.
- `cmd/backup.go` runs a full `doctor.Check()` at the start of its `Run` function and
  exits with code `1` if any check fails, with a clear logged message per failed check.
- `cmd/doctor.go` runs `doctor.Check()` and displays all results via the TUI model —
  it does not exit on failure, it reports.
- Runtime errors (rclone failure, docker exec failure) exit with code `2`.

### Exit codes

| Code | Meaning |
|------|---------|
| `0`  | Success |
| `1`  | Prerequisite or config failure |
| `2`  | Runtime failure during backup or daemon operation |

### Logging

Stdlib `log/slog` (Go 1.21+, zero CGo, no extra dependencies). Two handlers active
simultaneously:

```go
// JSON to log file — for daemon/machine consumption
fileHandler := slog.NewJSONHandler(logFile, nil)
// Human-readable text to stderr — for interactive use
termHandler := slog.NewTextHandler(os.Stderr, nil)
```

Every fatal failure logs at `ERROR` with a `remedy` attribute before `os.Exit`:

```go
slog.Error("docker socket unreachable",
    "error", err,
    "remedy", "ensure Docker is running and your user is in the docker group")
os.Exit(1)
```

## Section 5: Testing Approach

### Philosophy

No mocks, no fakes, no hand-written stubs. All tests that exercise external
dependencies run against real infrastructure using
[testcontainers-go](https://golang.testcontainers.org/). No build tags — a plain
`go test ./...` runs everything.

### Database backup tests (`internal/backup`, `internal/docker`)

- Spin up a real `postgres:alpine` container via testcontainers-go.
- Run `pg_dumpall` through `docker exec` against it.
- Assert the dump file exists, is non-empty, and is valid gzip.

### Media backup tests (`internal/backup`)

- Create a temp directory with dummy files (small images/text files).
- Configure a `local:` rclone remote pointing to a temp destination directory.
- Run `rclone sync` via the backup runner using the real rclone binary.
- Assert destination contains exactly the expected files with matching sizes and mtimes.

### Doctor tests (`internal/doctor`)

- Spin up the postgres container and assert all `CheckResult` entries have `OK: true`.
- Stop/remove the container and assert the correct `CheckResult` failures are returned
  with non-empty `Remedy` fields.
- Assert that a missing rclone binary produces a `CheckResult` with `OK: false` for
  the rclone check (temporarily rename the binary or use a temp PATH in the test).

### Config tests (`internal/config`)

- No containers needed — pure filesystem I/O against temp directories.
- Table-driven cases: missing file (auto-create with defaults), missing required field,
  invalid cron expression, malformed YAML.

### Status tests (`internal/status`)

- Write a `LastRun` struct to a temp file, read it back, assert round-trip equality.

### Daemon tests (`internal/daemon`)

- **In scope for skeleton**: Test `launchd.go` and `systemd.go` generation functions
  (plist and unit file content) as string assertions — no OS calls needed.
- **Explicitly deferred**: `Manager` interface methods `Install`, `Uninstall`, `Start`,
  `Stop`, `Restart`, `Status`, and `Logs` interact with OS-level service managers
  (launchd on macOS, systemd on Linux) that are not available in a generic CI
  environment. These are intentionally stubbed in the skeleton with
  `return fmt.Errorf("not implemented")` and will be tested manually on each target
  platform before the first release.

### Test execution

```bash
# All tests (requires Docker socket and rclone binary accessible):
go test ./...

# Verify no CGo:
CGO_ENABLED=0 go test ./...
```
