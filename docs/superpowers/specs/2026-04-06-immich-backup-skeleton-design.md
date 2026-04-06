# Design Spec: immich-backup Go Skeleton

**Date**: 2026-04-06
**Status**: Approved
**Module**: `github.com/daksh7011/immich-backup`

## Overview

A Go CLI tool for backing up Immich (self-hosted photo server) data using rclone.
Distributed as a standalone binary via Homebrew (macOS/Linux) and Chocolatey (Windows).
Built with Cobra for CLI, Bubble Tea + Lip Gloss + Huh for TUI, and yaml.v3 for config.

## Constitution Compliance

All design decisions comply with the `immich-backup` constitution v1.0.0:

- No CGo. `CGO_ENABLED=0` throughout.
- Rclone remote name is the only coupling point to storage backends.
- Fail-fast before any backup if rclone, Docker socket, or Postgres container is unreachable.
- Database backup exclusively via `pg_dumpall` through `docker exec`.
- Rclone configuration is never created or modified by this tool.
- Daemon management via launchd (macOS) and systemd user service (Linux). No root required.

## Section 1: Directory Structure

```
immich-backup/
├── main.go                          # entry point: calls cmd.Execute()
├── go.mod                           # module github.com/daksh7011/immich-backup
├── go.sum
│
├── cmd/
│   ├── root.go                      # root command, global flags, config init
│   ├── setup.go                     # interactive first-run wizard
│   ├── configure.go                 # re-run any part of setup
│   ├── backup.go                    # run a backup now
│   ├── status.go                    # last backup status + next scheduled run
│   ├── doctor.go                    # check all prerequisites
│   ├── logs.go                      # tail/show logs
│   └── daemon.go                    # sub-commands: install/uninstall/start/stop/restart/status/logs
│
├── internal/
│   ├── config/
│   │   ├── config.go                # Config struct + Load/Save/Validate using yaml.v3
│   │   └── config_test.go
│   ├── backup/
│   │   ├── backup.go                # orchestrates media sync + db dump
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
│   │   ├── doctor.go                # prerequisite checks (rclone, docker, container)
│   │   └── doctor_test.go
│   └── tui/
│       ├── model.go                 # shared Bubble Tea types/helpers
│       ├── setup_model.go           # Huh form model for setup wizard
│       ├── configure_model.go
│       ├── backup_model.go          # progress display during backup
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

### Core interfaces

```go
// internal/docker
type Executor interface {
    Exec(container, command string, args ...string) ([]byte, error)
    IsContainerRunning(name string) (bool, error)
}

// internal/backup
type Runner interface {
    RunMedia(remote, srcDir string) error
    RunDatabase(container, destPath string) error
}

// internal/doctor
type CheckResult struct {
    Name    string
    OK      bool
    Message string
    Remedy  string
}
func Check(exec docker.Executor, cfg *config.Config) []CheckResult
```

### Backup data flow

```
cmd/backup.go
  → doctor.Check()           // fail-fast: abort if any check fails
  → backup.RunDatabase()     // docker.Exec(pg_dumpall) → gzip → local tmp file
  → backup.RunMedia()        // exec: rclone sync <remote>:<path>
  → tui/backup_model.go      // streams progress back to Bubble Tea model
```

### Config flow

```
cmd/root.go (PersistentPreRun)
  → config.Load("~/.immich-backup/config.yaml")
  → stored in cobra.Command context
  → passed to each sub-command handler
```

`setup` and `configure` are exempt from the `PersistentPreRun` doctor check — they
are designed to run even when config or prerequisites are broken.

## Section 3: Config Schema

### File location

`~/.immich-backup/config.yaml`

### Format

```yaml
rclone_remote: "myremote:immich-backups"    # remote:path — no provider flags ever
immich_data_dir: "/path/to/immich/library"  # source dir for media sync
postgres_container: "immich_postgres"        # docker container name
schedule: "0 2 * * *"                        # cron expression for daemon
log_file: "~/.immich-backup/backup.log"      # path to log file
```

### Go struct

```go
type Config struct {
    RcloneRemote      string `yaml:"rclone_remote"`
    ImmichDataDir     string `yaml:"immich_data_dir"`
    PostgresContainer string `yaml:"postgres_container"`
    Schedule          string `yaml:"schedule"`
    LogFile           string `yaml:"log_file"`
}
```

### Load-or-initialize behavior

If the config file does not exist, `Load()` writes the defaults to disk and returns
them. No separate init step is required.

```go
var defaults = Config{
    RcloneRemote:      "myremote:immich-backups",
    ImmichDataDir:     "/path/to/immich/library",
    PostgresContainer: "immich_postgres",
    Schedule:          "0 2 * * *",
    LogFile:           "~/.immich-backup/backup.log",
}

func Load(path string) (*Config, error) {
    if _, err := os.Stat(path); os.IsNotExist(err) {
        cfg := defaults
        return &cfg, Save(path, &cfg)
    }
    // unmarshal and validate existing file
}
```

### Strict validation

`Load()` calls `Validate()` after every unmarshal. Any malformed or missing field
is a hard error — the program exits with code 1 and a clear message listing every
failed field. The user can fix via `immich-backup configure` or by editing the file
manually. `doctor` surfaces these errors with remediation hints.

```go
func (c *Config) Validate() error {
    var errs []string
    if c.RcloneRemote == ""      { errs = append(errs, "rclone_remote is required") }
    if c.ImmichDataDir == ""     { errs = append(errs, "immich_data_dir is required") }
    if c.PostgresContainer == "" { errs = append(errs, "postgres_container is required") }
    if c.Schedule == ""          { errs = append(errs, "schedule is required") }
    if !validCron(c.Schedule)    { errs = append(errs, "schedule is not a valid cron expression") }
    if c.LogFile == ""           { errs = append(errs, "log_file is required") }
    if len(errs) > 0 {
        return fmt.Errorf("config validation failed:\n  - %s", strings.Join(errs, "\n  - "))
    }
    return nil
}
```

Doctor check output example:

```
✗  Config       invalid: schedule is not a valid cron expression
   → Run `immich-backup configure` to fix, or edit ~/.immich-backup/config.yaml manually
```

## Section 4: Error Handling & Logging

### Error handling strategy

- `PersistentPreRun` on the root command calls `doctor.Check()` before every command
  except `setup` and `configure`.
- Any failed check exits immediately with code `1` and a descriptive log message.
- Runtime errors (rclone failure, docker exec failure) exit with code `2`.

### Exit codes

| Code | Meaning |
|------|---------|
| `0`  | Success |
| `1`  | Prerequisite or config failure (doctor-catchable) |
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

No mocks, no fakes, no hand-written stubs. All tests run against real infrastructure
using [testcontainers-go](https://golang.testcontainers.org/). If a test cannot run
against the real system, it does not exist.

### Database backup tests (`internal/backup`, `internal/docker`)

- Spin up a real `postgres:alpine` container via testcontainers-go.
- Run `pg_dumpall` through `docker exec` against it.
- Assert the dump file exists, is non-empty, and is valid gzip.

### Media backup tests (`internal/backup`)

- Create a temp directory with dummy files (small images/text files).
- Configure a `local:` rclone remote pointing to a temp destination directory.
- Run `rclone sync` via the backup runner.
- Assert destination contains exactly the expected files with matching sizes and mtimes.

### Doctor tests (`internal/doctor`)

- Spin up the postgres container and assert all `CheckResult` entries have `OK: true`.
- Stop/remove the container and assert the correct `CheckResult` failures are returned
  with non-empty `Remedy` fields.

### Config tests (`internal/config`)

- No containers needed — pure filesystem I/O against temp directories.
- Table-driven cases: missing file (auto-create with defaults), missing required field,
  invalid cron expression, malformed YAML.

### Test execution

```bash
# All tests (requires Docker socket accessible):
go test ./...

# Verify no CGo:
CGO_ENABLED=0 go test ./...
```

No build tags. The whole project assumes Docker is a prerequisite, so tests do too.
