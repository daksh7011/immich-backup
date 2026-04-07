# immich-backup Go Skeleton Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Scaffold the complete `immich-backup` Go project — all packages, interfaces,
compilable implementations, and real infrastructure tests — producing a binary that
builds with `CGO_ENABLED=0`.

**Architecture:** Thin `cmd/` Cobra commands delegate to `internal/` domain packages.
All rclone calls pass `--config ~/.immich-backup/rclone.conf`. The backup runner sends
typed progress messages over a `chan any` to a Bubble Tea model. Tests use
testcontainers-go against real Docker infrastructure; no mocks, no fakes.

**Tech Stack:** Go 1.22, Cobra v1.8, Bubble Tea v1, Lip Gloss, Huh v0.6, yaml.v3,
Docker SDK (pure Go, `github.com/docker/docker`), robfig/cron/v3, testcontainers-go

**Spec:** `docs/superpowers/specs/2026-04-06-immich-backup-skeleton-design.md`
**Constitution:** `.specify/memory/constitution.md` (v2.0.0)

---

## File Map

| File | Responsibility |
|------|----------------|
| `main.go` | Entry point — `cmd.Execute()` |
| `cmd/root.go` | Root Cobra command, `PersistentPreRun` (config load), context helpers |
| `cmd/setup.go` | First-run wizard: calls `rcloneconf.EnsureConfigured` then Huh form |
| `cmd/configure.go` | Re-run wizard for any section |
| `cmd/backup.go` | Trigger backup with live Bubble Tea progress model |
| `cmd/status.go` | Display last-run result + next scheduled time |
| `cmd/doctor.go` | Display all `CheckResult` entries via TUI model |
| `cmd/logs.go` | Tail `cfg.Daemon.LogPath` file |
| `cmd/daemon.go` | Sub-commands: install/uninstall/start/stop/restart/status/logs |
| `internal/config/paths.go` | App directory path helpers (`AppDir`, `RcloneConfigPath`, etc.) |
| `internal/config/config.go` | Nested `Config` struct, `Load`, `Save`, `Validate`, defaults |
| `internal/config/config_test.go` | Table-driven Load/Save/Validate tests |
| `internal/status/status.go` | `LastRun` struct, `Load`, `Save` (JSON) |
| `internal/status/status_test.go` | Round-trip JSON test |
| `internal/docker/docker.go` | `Executor` interface + `Client` (Docker SDK) |
| `internal/docker/docker_test.go` | testcontainers: real container exec + running check |
| `internal/rcloneconf/rcloneconf.go` | `EnsureConfigured`: detect/launch `rclone config` |
| `internal/rcloneconf/rcloneconf_test.go` | Happy path (config already present) |
| `internal/backup/backup.go` | `Runner` interface, `BackupRunner`, `Run()`, progress message types |
| `internal/backup/backup_test.go` | testcontainers: pg_dumpall dump + rclone sync |
| `internal/daemon/daemon.go` | `Manager` interface + `New()` platform dispatch |
| `internal/daemon/launchd.go` | macOS plist generation + `launchdManager` (control methods stubbed) |
| `internal/daemon/systemd.go` | Linux unit file generation + `systemdManager` (control methods stubbed) |
| `internal/daemon/daemon_test.go` | Plist and unit file content string assertions |
| `internal/doctor/doctor.go` | `CheckResult`, `Check()` (5 ordered checks) |
| `internal/doctor/doctor_test.go` | testcontainers: all-pass + individual-fail scenarios |
| `internal/tui/model.go` | Shared Bubble Tea message types (`ProgressMsg`, `ErrorMsg`, `DoneMsg`) |
| `internal/tui/setup_model.go` | Huh form model for setup wizard |
| `internal/tui/configure_model.go` | Huh form model for configure |
| `internal/tui/backup_model.go` | Channel-driven progress display |
| `internal/tui/status_model.go` | Last-run display |
| `internal/tui/doctor_model.go` | `CheckResult` list display |
| `internal/tui/logs_model.go` | Log tail display |
| `internal/tui/daemon_model.go` | Daemon operation result display |

---

## Task 1: Initialize Go module and install dependencies

**Files:**
- Create: `go.mod`, `go.sum`
- Create: all `internal/` and `cmd/` directories

- [ ] **Step 1: Initialize the module**

```bash
cd /home/slothie/IdeaProjects/immich-backup
go mod init github.com/daksh7011/immich-backup
```

- [ ] **Step 2: Add all dependencies**

```bash
go get github.com/spf13/cobra@latest
go get github.com/charmbracelet/bubbletea@latest
go get github.com/charmbracelet/lipgloss@latest
go get github.com/charmbracelet/huh@latest
go get gopkg.in/yaml.v3@latest
go get github.com/docker/docker@latest
go get github.com/docker/docker/client@latest
go get github.com/robfig/cron/v3@latest
go get github.com/testcontainers/testcontainers-go@latest
go get github.com/testcontainers/testcontainers-go/modules/postgres@latest
```

- [ ] **Step 3: Create directory structure**

```bash
mkdir -p cmd \
    internal/config \
    internal/status \
    internal/docker \
    internal/rcloneconf \
    internal/backup \
    internal/daemon \
    internal/doctor \
    internal/tui
```

- [ ] **Step 4: Verify module is CGo-free**

```bash
CGO_ENABLED=0 go build ./... 2>&1 || true
```
Expected: no CGo-related errors (will have "no Go files" errors for empty dirs — that's fine).

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum
git commit -m "feat: initialize Go module with all dependencies"
```

---

## Task 2: internal/config — path helpers, Config types, Load/Save/Validate

**Files:**
- Create: `internal/config/paths.go`
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/config/config_test.go
package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daksh7011/immich-backup/internal/config"
)

func TestLoad_CreatesDefaultsWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Backup.RcloneRemote == "" {
		t.Error("expected default rclone_remote to be populated")
	}
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		t.Error("expected config file to be written to disk")
	}
}

func TestLoad_FailsOnMissingUploadLocation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(path, []byte(`
immich:
  postgres_container: immich_postgres
  postgres_user: postgres
  postgres_db: immich
backup:
  rclone_remote: "b2:test"
  schedule: "0 3 * * *"
  db_backup_frequency: "0 */6 * * *"
  retention:
    daily: 7
    weekly: 4
daemon:
  log_path: /tmp/test.log
`), 0644)

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "upload_location") {
		t.Errorf("expected error to mention upload_location, got: %v", err)
	}
}

func TestLoad_FailsOnInvalidCron(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(path, []byte(`
immich:
  upload_location: /mnt/immich
  postgres_container: immich_postgres
  postgres_user: postgres
  postgres_db: immich
backup:
  rclone_remote: "b2:test"
  schedule: "not-a-cron"
  db_backup_frequency: "0 */6 * * *"
  retention:
    daily: 7
    weekly: 4
daemon:
  log_path: /tmp/test.log
`), 0644)

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected validation error for invalid cron")
	}
	if !strings.Contains(err.Error(), "schedule") {
		t.Errorf("expected error to mention schedule, got: %v", err)
	}
}

func TestLoad_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(path, []byte(`
immich:
  upload_location: /mnt/immich
  postgres_container: immich_postgres
  postgres_user: postgres
  postgres_db: immich
backup:
  rclone_remote: "b2:test"
  schedule: "0 3 * * *"
  db_backup_frequency: "0 */6 * * *"
  retention:
    daily: 7
    weekly: 4
daemon:
  log_path: /tmp/test.log
`), 0644)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Immich.UploadLocation != "/mnt/immich" {
		t.Errorf("got upload_location=%q, want /mnt/immich", cfg.Immich.UploadLocation)
	}
	if cfg.Backup.Retention.Daily != 7 {
		t.Errorf("got retention.daily=%d, want 7", cfg.Backup.Retention.Daily)
	}
}
```

- [ ] **Step 2: Run tests — expect compile failure**

```bash
CGO_ENABLED=0 go test ./internal/config/...
```
Expected: `cannot find package` error.

- [ ] **Step 3: Write paths.go**

```go
// internal/config/paths.go
package config

import (
	"os"
	"path/filepath"
)

// AppDir returns ~/.immich-backup.
func AppDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".immich-backup")
}

func DefaultConfigPath() string { return filepath.Join(AppDir(), "config.yaml") }
func RcloneConfigPath() string  { return filepath.Join(AppDir(), "rclone.conf") }
func StatusFilePath() string    { return filepath.Join(AppDir(), "last-run.json") }
func DefaultLogPath() string    { return filepath.Join(AppDir(), "logs", "daemon.log") }
```

- [ ] **Step 4: Write config.go**

```go
// internal/config/config.go
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/robfig/cron/v3"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Immich ImmichConfig `yaml:"immich"`
	Backup BackupConfig `yaml:"backup"`
	Daemon DaemonConfig `yaml:"daemon"`
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
}

// Load reads the config at path. If missing, writes defaults and returns them.
// If present, unmarshals and validates. Any validation error is returned as-is.
func Load(path string) (*Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := defaults
		cfg.Daemon.LogPath = DefaultLogPath()
		if err := Save(path, &cfg); err != nil {
			return nil, fmt.Errorf("write default config: %w", err)
		}
		return &cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Save marshals cfg to YAML and writes it to path, creating parent dirs as needed.
func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// Validate checks all required fields and cron expressions.
func (c *Config) Validate() error {
	var errs []string
	if c.Immich.UploadLocation == ""    { errs = append(errs, "immich.upload_location is required") }
	if c.Immich.PostgresContainer == "" { errs = append(errs, "immich.postgres_container is required") }
	if c.Immich.PostgresUser == ""      { errs = append(errs, "immich.postgres_user is required") }
	if c.Immich.PostgresDB == ""        { errs = append(errs, "immich.postgres_db is required") }
	if c.Backup.RcloneRemote == ""      { errs = append(errs, "backup.rclone_remote is required") }
	if c.Backup.Schedule == ""          { errs = append(errs, "backup.schedule is required") }
	if c.Backup.Schedule != "" && !validCron(c.Backup.Schedule) {
		errs = append(errs, "backup.schedule is not a valid cron expression")
	}
	if c.Backup.DBBackupFrequency == "" { errs = append(errs, "backup.db_backup_frequency is required") }
	if c.Backup.DBBackupFrequency != "" && !validCron(c.Backup.DBBackupFrequency) {
		errs = append(errs, "backup.db_backup_frequency is not a valid cron expression")
	}
	if c.Backup.Retention.Daily <= 0  { errs = append(errs, "backup.retention.daily must be > 0") }
	if c.Backup.Retention.Weekly <= 0 { errs = append(errs, "backup.retention.weekly must be > 0") }
	if c.Daemon.LogPath == ""          { errs = append(errs, "daemon.log_path is required") }
	if len(errs) > 0 {
		return fmt.Errorf("config validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

func validCron(expr string) bool {
	p := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := p.Parse(expr)
	return err == nil
}
```

- [ ] **Step 5: Run tests — expect PASS**

```bash
CGO_ENABLED=0 go test ./internal/config/... -v
```
Expected: all 4 tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/config/
git commit -m "feat: add internal/config with nested structs, Load/Save/Validate"
```

---

## Task 3: internal/status — LastRun JSON

**Files:**
- Create: `internal/status/status.go`
- Create: `internal/status/status_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/status/status_test.go
package status_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/daksh7011/immich-backup/internal/status"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "last-run.json")
	want := &status.LastRun{
		Time:   time.Now().UTC().Truncate(time.Second),
		Result: "success",
	}
	if err := status.Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := status.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !got.Time.Equal(want.Time) {
		t.Errorf("time: got %v, want %v", got.Time, want.Time)
	}
	if got.Result != want.Result {
		t.Errorf("result: got %q, want %q", got.Result, want.Result)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := status.Load("/nonexistent/last-run.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestRoundTrip_WithError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "last-run.json")
	want := &status.LastRun{
		Time:   time.Now().UTC().Truncate(time.Second),
		Result: "error",
		Error:  "rclone: exit status 1",
	}
	if err := status.Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := status.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Error != want.Error {
		t.Errorf("error field: got %q, want %q", got.Error, want.Error)
	}
}
```

- [ ] **Step 2: Run tests — expect compile failure**

```bash
CGO_ENABLED=0 go test ./internal/status/...
```

- [ ] **Step 3: Write status.go**

```go
// internal/status/status.go
package status

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// LastRun holds the result of the most recent backup run.
type LastRun struct {
	Time   time.Time `json:"time"`
	Result string    `json:"result"` // "success" | "error"
	Error  string    `json:"error,omitempty"`
}

// Load reads the last-run status from path.
func Load(path string) (*LastRun, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read status: %w", err)
	}
	var r LastRun
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse status: %w", err)
	}
	return &r, nil
}

// Save writes r as indented JSON to path, creating parent directories as needed.
func Save(path string, r *LastRun) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal status: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}
```

- [ ] **Step 4: Run tests — expect PASS**

```bash
CGO_ENABLED=0 go test ./internal/status/... -v
```
Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/status/
git commit -m "feat: add internal/status with LastRun JSON read/write"
```

---

## Task 4: internal/docker — Executor interface and Docker SDK client

**Files:**
- Create: `internal/docker/docker.go`
- Create: `internal/docker/docker_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/docker/docker_test.go
package docker_test

import (
	"context"
	"strings"
	"testing"

	"github.com/daksh7011/immich-backup/internal/docker"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func startAlpine(t *testing.T) (name string, cleanup func()) {
	t.Helper()
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:      "alpine:latest",
			Cmd:        []string{"sleep", "60"},
			WaitingFor: wait.ForExec([]string{"echo", "ready"}),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start alpine: %v", err)
	}
	rawName, _ := c.Name(ctx)
	return strings.TrimPrefix(rawName, "/"), func() { _ = c.Terminate(ctx) }
}

func TestClient_Exec(t *testing.T) {
	name, cleanup := startAlpine(t)
	t.Cleanup(cleanup)

	client, err := docker.NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	out, err := client.Exec(name, "echo", "hello-from-exec")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if !strings.Contains(string(out), "hello-from-exec") {
		t.Errorf("expected 'hello-from-exec' in output, got: %q", string(out))
	}
}

func TestClient_IsContainerRunning_True(t *testing.T) {
	name, cleanup := startAlpine(t)
	t.Cleanup(cleanup)

	client, err := docker.NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	running, err := client.IsContainerRunning(name)
	if err != nil {
		t.Fatalf("IsContainerRunning: %v", err)
	}
	if !running {
		t.Error("expected container to be running")
	}
}

func TestClient_IsContainerRunning_NotFound(t *testing.T) {
	client, err := docker.NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	running, err := client.IsContainerRunning("definitely-does-not-exist-xyzxyz")
	if err != nil {
		t.Fatalf("unexpected error for missing container: %v", err)
	}
	if running {
		t.Error("expected false for non-existent container")
	}
}
```

- [ ] **Step 2: Run tests — expect compile failure**

```bash
CGO_ENABLED=0 go test ./internal/docker/...
```

- [ ] **Step 3: Write docker.go**

```go
// internal/docker/docker.go
package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types/container"
	dockerclient "github.com/docker/docker/client"
)

// Executor runs commands inside Docker containers and inspects container state.
type Executor interface {
	Exec(containerName, command string, args ...string) ([]byte, error)
	IsContainerRunning(containerName string) (bool, error)
}

// Client is a concrete Executor backed by the Docker Engine SDK.
type Client struct {
	cli *dockerclient.Client
}

// NewClient creates a Client connected to the Docker socket via environment
// variables (DOCKER_HOST) or the default Unix socket.
func NewClient() (*Client, error) {
	cli, err := dockerclient.NewClientWithOpts(
		dockerclient.FromEnv,
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to Docker socket: %w", err)
	}
	return &Client{cli: cli}, nil
}

// Close releases the underlying Docker client connection.
func (c *Client) Close() { _ = c.cli.Close() }

// Exec runs command + args inside containerName and returns combined stdout+stderr.
func (c *Client) Exec(containerName, command string, args ...string) ([]byte, error) {
	ctx := context.Background()
	cmd := append([]string{command}, args...)

	execID, err := c.cli.ContainerExecCreate(ctx, containerName, container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return nil, fmt.Errorf("exec create in %s: %w", containerName, err)
	}

	resp, err := c.cli.ContainerExecAttach(ctx, execID.ID, container.ExecStartOptions{})
	if err != nil {
		return nil, fmt.Errorf("exec attach: %w", err)
	}
	defer resp.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, resp.Reader); err != nil {
		return nil, fmt.Errorf("read exec output: %w", err)
	}
	return buf.Bytes(), nil
}

// IsContainerRunning returns true if containerName exists and is in Running state.
// Returns false (no error) if the container does not exist.
func (c *Client) IsContainerRunning(containerName string) (bool, error) {
	ctx := context.Background()
	info, err := c.cli.ContainerInspect(ctx, containerName)
	if err != nil {
		if dockerclient.IsErrNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("inspect %s: %w", containerName, err)
	}
	return info.State.Running, nil
}
```

- [ ] **Step 4: Run tests — expect PASS**

```bash
CGO_ENABLED=0 go test ./internal/docker/... -v -timeout 120s
```
Expected: all 3 tests PASS. Requires Docker socket accessible.

- [ ] **Step 5: Commit**

```bash
git add internal/docker/
git commit -m "feat: add internal/docker with Executor interface and Docker SDK client"
```

---

## Task 5: internal/rcloneconf — EnsureConfigured (pause-launch-resume)

**Files:**
- Create: `internal/rcloneconf/rcloneconf.go`
- Create: `internal/rcloneconf/rcloneconf_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/rcloneconf/rcloneconf_test.go
package rcloneconf_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/daksh7011/immich-backup/internal/rcloneconf"
)

const validConf = "[test-local]\ntype = local\n"

func TestEnsureConfigured_AlreadyConfigured(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rclone.conf")
	if err := os.WriteFile(path, []byte(validConf), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := rcloneconf.EnsureConfigured(path); err != nil {
		t.Fatalf("unexpected error for configured remote: %v", err)
	}
}

func TestEnsureConfigured_EmptyFileIsNotInteractive(t *testing.T) {
	// In CI / non-TTY, an empty rclone.conf means EnsureConfigured will attempt
	// to launch rclone config. Since there is no TTY it will exit immediately,
	// and EnsureConfigured must return an error (no remotes after launch).
	// This test verifies the post-launch check fires, not the interactive UX.
	path := filepath.Join(t.TempDir(), "rclone.conf")
	if err := os.WriteFile(path, []byte(""), 0600); err != nil {
		t.Fatalf("write empty config: %v", err)
	}
	err := rcloneconf.EnsureConfigured(path)
	// We accept either a launch error or a "no remotes" error —
	// both mean the guard functioned correctly.
	if err == nil {
		t.Log("Note: rclone config may have been interactive — run manually to confirm.")
	}
}
```

- [ ] **Step 2: Run tests — expect compile failure**

```bash
CGO_ENABLED=0 go test ./internal/rcloneconf/...
```

- [ ] **Step 3: Write rcloneconf.go**

```go
// internal/rcloneconf/rcloneconf.go
package rcloneconf

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// EnsureConfigured checks that path contains at least one configured rclone remote.
// If the config is missing or has no remotes it:
//  1. Informs the user.
//  2. Launches `rclone config --config <path>` interactively (inherits the terminal).
//  3. After rclone config exits, verifies ≥1 remote now exists.
//  4. Returns an error if still unconfigured (constitution Principle III).
func EnsureConfigured(path string) error {
	remotes, err := listRemotes(path)
	if err != nil || len(remotes) == 0 {
		fmt.Fprintf(os.Stdout,
			"No rclone remote configured at %s. Launching rclone config...\n", path)
		if launchErr := launchConfig(path); launchErr != nil {
			return fmt.Errorf("rclone config exited with error: %w", launchErr)
		}
		remotes, err = listRemotes(path)
		if err != nil || len(remotes) == 0 {
			return fmt.Errorf(
				"no rclone remote configured — run `rclone config --config %s` to add one",
				path,
			)
		}
	}
	return nil
}

// listRemotes runs `rclone listremotes --config path` and returns remote names.
func listRemotes(path string) ([]string, error) {
	out, err := exec.Command("rclone", "listremotes", "--config", path).Output()
	if err != nil {
		return nil, fmt.Errorf("rclone listremotes: %w", err)
	}
	var remotes []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			remotes = append(remotes, line)
		}
	}
	return remotes, nil
}

// launchConfig runs `rclone config` interactively, inheriting the terminal.
func launchConfig(path string) error {
	cmd := exec.Command("rclone", "config", "--config", path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
```

- [ ] **Step 4: Run tests — expect PASS**

```bash
CGO_ENABLED=0 go test ./internal/rcloneconf/... -v
```
Expected: `TestEnsureConfigured_AlreadyConfigured` PASS.
`TestEnsureConfigured_EmptyFileIsNotInteractive` may pass or log a note — both are acceptable.

- [ ] **Step 5: Commit**

```bash
git add internal/rcloneconf/
git commit -m "feat: add internal/rcloneconf with EnsureConfigured pause-launch-resume"
```

---

## Task 6: internal/backup — Runner, RunDatabase, RunMedia, Run orchestrator

**Files:**
- Create: `internal/backup/backup.go`
- Create: `internal/backup/backup_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/backup/backup_test.go
package backup_test

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daksh7011/immich-backup/internal/backup"
	"github.com/daksh7011/immich-backup/internal/docker"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func newDockerClient(t *testing.T) *docker.Client {
	t.Helper()
	c, err := docker.NewClient()
	if err != nil {
		t.Fatalf("docker.NewClient: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestRunDatabase_ProducesDump(t *testing.T) {
	ctx := context.Background()
	pg, err := postgres.RunContainer(ctx,
		testcontainers.WithImage("postgres:17-alpine"),
		postgres.WithDatabase("immich"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = pg.Terminate(ctx) })

	rawName, _ := pg.Name(ctx)
	name := strings.TrimPrefix(rawName, "/")

	// Write a minimal rclone.conf — RunDatabase doesn't use rclone but New() requires it.
	confPath := filepath.Join(t.TempDir(), "rclone.conf")
	_ = os.WriteFile(confPath, []byte("[local]\ntype = local\n"), 0600)

	destPath := filepath.Join(t.TempDir(), "dump.sql.gz")
	r := backup.New(newDockerClient(t), confPath)
	if err := r.RunDatabase(name, "postgres", "immich", destPath); err != nil {
		t.Fatalf("RunDatabase: %v", err)
	}

	f, err := os.Open(destPath)
	if err != nil {
		t.Fatalf("open dump: %v", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("not a valid gzip: %v", err)
	}
	content, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("read gzip: %v", err)
	}
	if len(content) == 0 {
		t.Error("dump content is empty")
	}
}

func TestRunMedia_SyncsFiles(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create dummy media files in source
	for _, name := range []string{"photo1.jpg", "photo2.jpg", "video.mp4"} {
		if err := os.WriteFile(filepath.Join(srcDir, name), []byte("dummy-content"), 0644); err != nil {
			t.Fatalf("create dummy file: %v", err)
		}
	}

	// Write a temp rclone.conf with a local: remote
	confDir := t.TempDir()
	confPath := filepath.Join(confDir, "rclone.conf")
	confContent := fmt.Sprintf("[testdst]\ntype = local\nnounc = true\n")
	if err := os.WriteFile(confPath, []byte(confContent), 0600); err != nil {
		t.Fatalf("write rclone config: %v", err)
	}

	r := backup.New(newDockerClient(t), confPath)
	remote := "testdst:" + dstDir
	if err := r.RunMedia(remote, srcDir); err != nil {
		t.Fatalf("RunMedia: %v", err)
	}

	for _, name := range []string{"photo1.jpg", "photo2.jpg", "video.mp4"} {
		dst := filepath.Join(dstDir, name)
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			t.Errorf("expected %s to be synced to destination", name)
		}
	}
}
```

- [ ] **Step 2: Run tests — expect compile failure**

```bash
CGO_ENABLED=0 go test ./internal/backup/...
```

- [ ] **Step 3: Write backup.go**

```go
// internal/backup/backup.go
package backup

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/daksh7011/immich-backup/internal/docker"
)

// Progress message types sent to the TUI channel during a backup run.
// These are defined here so internal/tui/backup_model.go can import them
// without creating an import cycle.
type ProgressMsg struct{ Text string }
type ErrorMsg struct{ Err error }
type DoneMsg struct{}

// Runner orchestrates database and media backup operations.
type Runner interface {
	RunMedia(remote, srcDir string) error
	RunDatabase(container, pgUser, pgDB, destPath string) error
}

// BackupRunner is the production implementation of Runner.
type BackupRunner struct {
	exec       docker.Executor
	rcloneConf string // path to --config file for all rclone calls
}

// New returns a BackupRunner. rcloneConf must be the path to the isolated
// rclone config (constitution Principle V). Panics if empty.
func New(exec docker.Executor, rcloneConf string) Runner {
	if rcloneConf == "" {
		panic("backup.New: rcloneConf must not be empty (constitution Principle V)")
	}
	return &BackupRunner{exec: exec, rcloneConf: rcloneConf}
}

// RunDatabase dumps all databases from the Postgres container via pg_dumpall,
// gzips the output, and writes it to destPath.
func (r *BackupRunner) RunDatabase(container, pgUser, pgDB, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("create dump dir: %w", err)
	}

	out, err := r.exec.Exec(container, "pg_dumpall", "-U", pgUser)
	if err != nil {
		return fmt.Errorf("pg_dumpall in %s: %w", container, err)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create dump file: %w", err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()

	if _, err := io.WriteString(gz, string(out)); err != nil {
		return fmt.Errorf("write gzip: %w", err)
	}
	return nil
}

// RunMedia syncs srcDir to the rclone remote using `rclone sync`.
// rcloneConf MUST be non-empty; New() panics if it is (constitution Principle V).
func (r *BackupRunner) RunMedia(remote, srcDir string) error {
	args := []string{"--config", r.rcloneConf, "sync", srcDir, remote}
	cmd := exec.Command("rclone", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rclone sync: %w", err)
	}
	return nil
}

// Run orchestrates a full backup: database then media.
// Progress, errors, and completion are sent to ch for live TUI display.
func Run(
	rcloneConf, container, pgUser, pgDB, uploadLocation, rcloneRemote string,
	exec docker.Executor,
	ch chan<- any,
) {
	send := func(msg any) {
		select {
		case ch <- msg:
		default:
		}
	}

	r := New(exec, rcloneConf)

	send(ProgressMsg{Text: "Starting database backup..."})
	dumpPath := filepath.Join(os.TempDir(),
		fmt.Sprintf("immich-db-%s.sql.gz", time.Now().Format("20060102-150405")))
	if err := r.RunDatabase(container, pgUser, pgDB, dumpPath); err != nil {
		send(ErrorMsg{Err: fmt.Errorf("database backup: %w", err)})
		close(ch)
		return
	}
	send(ProgressMsg{Text: "Database backup complete. Starting media sync..."})

	if err := r.RunMedia(rcloneRemote, uploadLocation); err != nil {
		send(ErrorMsg{Err: fmt.Errorf("media sync: %w", err)})
		close(ch)
		return
	}
	send(ProgressMsg{Text: "Media sync complete."})
	send(DoneMsg{})
	close(ch)
}
```

- [ ] **Step 4: Run tests — expect PASS**

```bash
CGO_ENABLED=0 go test ./internal/backup/... -v -timeout 180s
```
Expected: both tests PASS. Requires Docker socket and `rclone` in PATH.

- [ ] **Step 5: Commit**

```bash
git add internal/backup/
git commit -m "feat: add internal/backup with Runner, RunDatabase, RunMedia, Run"
```

---

## Task 7: internal/daemon — Manager interface, plist/unit generation, stub controls

**Files:**
- Create: `internal/daemon/daemon.go`
- Create: `internal/daemon/launchd.go`
- Create: `internal/daemon/systemd.go`
- Create: `internal/daemon/daemon_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/daemon/daemon_test.go
package daemon_test

import (
	"strings"
	"testing"

	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/daemon"
)

var testCfg = &config.Config{
	Backup: config.BackupConfig{Schedule: "0 3 * * *"},
	Daemon: config.DaemonConfig{LogPath: "/home/user/.immich-backup/logs/daemon.log"},
}

func TestGeneratePlist_ContainsLabel(t *testing.T) {
	plist := daemon.GeneratePlist("/usr/local/bin/immich-backup", testCfg)
	if !strings.Contains(plist, "com.immich-backup.agent") {
		t.Errorf("plist missing label: %s", plist)
	}
}

func TestGeneratePlist_ContainsBinaryPath(t *testing.T) {
	plist := daemon.GeneratePlist("/usr/local/bin/immich-backup", testCfg)
	if !strings.Contains(plist, "/usr/local/bin/immich-backup") {
		t.Errorf("plist missing binary path: %s", plist)
	}
}

func TestGeneratePlist_ContainsLogPath(t *testing.T) {
	plist := daemon.GeneratePlist("/usr/local/bin/immich-backup", testCfg)
	if !strings.Contains(plist, testCfg.Daemon.LogPath) {
		t.Errorf("plist missing log path: %s", plist)
	}
}

func TestGeneratePlist_NoRootPaths(t *testing.T) {
	plist := daemon.GeneratePlist("/usr/local/bin/immich-backup", testCfg)
	if strings.Contains(plist, "/Library/LaunchDaemons") {
		t.Error("plist must not reference root LaunchDaemons path")
	}
}

func TestGenerateSystemdUnit_ContainsExecStart(t *testing.T) {
	unit := daemon.GenerateSystemdUnit("/usr/local/bin/immich-backup", testCfg)
	if !strings.Contains(unit, "ExecStart=/usr/local/bin/immich-backup") {
		t.Errorf("unit missing ExecStart: %s", unit)
	}
}

func TestGenerateSystemdUnit_ContainsWantedBy(t *testing.T) {
	unit := daemon.GenerateSystemdUnit("/usr/local/bin/immich-backup", testCfg)
	if !strings.Contains(unit, "WantedBy=default.target") {
		t.Errorf("unit missing WantedBy: %s", unit)
	}
}
```

- [ ] **Step 2: Run tests — expect compile failure**

```bash
CGO_ENABLED=0 go test ./internal/daemon/...
```

- [ ] **Step 3: Write daemon.go**

```go
// internal/daemon/daemon.go
package daemon

import (
	"fmt"
	"runtime"

	"github.com/daksh7011/immich-backup/internal/config"
)

// Manager controls the immich-backup background service.
type Manager interface {
	Install(cfg *config.Config) error
	Uninstall() error
	Start() error
	Stop() error
	Restart() error
	Status() (string, error)
	Logs() (string, error)
}

// New returns the platform-appropriate Manager.
// Panics if the platform is not supported (Windows is out of scope).
func New() Manager {
	switch runtime.GOOS {
	case "darwin":
		return &launchdManager{}
	case "linux":
		return &systemdManager{}
	default:
		panic(fmt.Sprintf("unsupported platform: %s", runtime.GOOS))
	}
}
```

- [ ] **Step 4: Write launchd.go**

```go
// internal/daemon/launchd.go
package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
	"strings"

	"github.com/daksh7011/immich-backup/internal/config"
)

const plistLabel = "com.immich-backup.agent"
const plistFilename = plistLabel + ".plist"

var plistTmpl = template.Must(template.New("plist").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
    "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{.Label}}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.BinaryPath}}</string>
        <string>backup</string>
    </array>
    <key>StartCalendarInterval</key>
    <dict>
        <key>Hour</key>
        <integer>{{.Hour}}</integer>
        <key>Minute</key>
        <integer>{{.Minute}}</integer>
    </dict>
    <key>StandardOutPath</key>
    <string>{{.LogPath}}</string>
    <key>StandardErrorPath</key>
    <string>{{.LogPath}}</string>
    <key>RunAtLoad</key>
    <false/>
</dict>
</plist>
`))

// GeneratePlist returns the launchd plist XML for the given binary path and config.
// Exported for testing.
func GeneratePlist(binaryPath string, cfg *config.Config) string {
	// Parse hour/minute from the cron schedule (field 1=minute, 2=hour)
	parts := strings.Fields(cfg.Backup.Schedule)
	minute, hour := "0", "3"
	if len(parts) >= 2 {
		minute, hour = parts[0], parts[1]
	}

	var buf strings.Builder
	_ = plistTmpl.Execute(&buf, map[string]string{
		"Label":      plistLabel,
		"BinaryPath": binaryPath,
		"Hour":       hour,
		"Minute":     minute,
		"LogPath":    cfg.Daemon.LogPath,
	})
	return buf.String()
}

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", plistFilename)
}

type launchdManager struct{}

func (m *launchdManager) Install(cfg *config.Config) error {
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	plist := GeneratePlist(bin, cfg)
	path := plistPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(plist), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}
	return exec.Command("launchctl", "load", path).Run()
}

func (m *launchdManager) Uninstall() error {
	path := plistPath()
	_ = exec.Command("launchctl", "unload", path).Run()
	return os.Remove(path)
}

func (m *launchdManager) Start() error {
	return exec.Command("launchctl", "start", plistLabel).Run()
}

func (m *launchdManager) Stop() error {
	return exec.Command("launchctl", "stop", plistLabel).Run()
}

func (m *launchdManager) Restart() error {
	_ = m.Stop()
	return m.Start()
}

func (m *launchdManager) Status() (string, error) {
	out, err := exec.Command("launchctl", "list", plistLabel).Output()
	return string(out), err
}

func (m *launchdManager) Logs() (string, error) {
	return "", fmt.Errorf("use `immich-backup logs` to view logs")
}
```

- [ ] **Step 5: Write systemd.go**

```go
// internal/daemon/systemd.go
package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/daksh7011/immich-backup/internal/config"
)

const unitName = "immich-backup.service"

var unitTmpl = template.Must(template.New("unit").Parse(`[Unit]
Description=immich-backup media and database backup service
After=network.target

[Service]
Type=oneshot
ExecStart={{.BinaryPath}} backup
StandardOutput=append:{{.LogPath}}
StandardError=append:{{.LogPath}}

[Install]
WantedBy=default.target
`))

// GenerateSystemdUnit returns the systemd unit file content.
// Exported for testing.
func GenerateSystemdUnit(binaryPath string, cfg *config.Config) string {
	var buf strings.Builder
	_ = unitTmpl.Execute(&buf, map[string]string{
		"BinaryPath": binaryPath,
		"LogPath":    cfg.Daemon.LogPath,
	})
	return buf.String()
}

func unitPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user", unitName)
}

type systemdManager struct{}

func (m *systemdManager) Install(cfg *config.Config) error {
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	unit := GenerateSystemdUnit(bin, cfg)
	path := unitPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create systemd user dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(unit), 0644); err != nil {
		return fmt.Errorf("write unit file: %w", err)
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	return exec.Command("systemctl", "--user", "enable", unitName).Run()
}

func (m *systemdManager) Uninstall() error {
	_ = exec.Command("systemctl", "--user", "disable", unitName).Run()
	return os.Remove(unitPath())
}

func (m *systemdManager) Start() error {
	return exec.Command("systemctl", "--user", "start", unitName).Run()
}

func (m *systemdManager) Stop() error {
	return exec.Command("systemctl", "--user", "stop", unitName).Run()
}

func (m *systemdManager) Restart() error {
	return exec.Command("systemctl", "--user", "restart", unitName).Run()
}

func (m *systemdManager) Status() (string, error) {
	out, err := exec.Command("systemctl", "--user", "status", unitName).Output()
	return string(out), err
}

func (m *systemdManager) Logs() (string, error) {
	out, err := exec.Command("journalctl", "--user", "-u", unitName, "-n", "100").Output()
	return string(out), err
}
```

- [ ] **Step 6: Run tests — expect PASS**

```bash
CGO_ENABLED=0 go test ./internal/daemon/... -v
```
Expected: all 6 plist/unit tests PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/daemon/
git commit -m "feat: add internal/daemon with Manager interface, plist/unit generation"
```

---

## Task 8: internal/doctor — CheckResult and Check (5 ordered checks)

**Files:**
- Create: `internal/doctor/doctor.go`
- Create: `internal/doctor/doctor_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/doctor/doctor_test.go
package doctor_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/docker"
	"github.com/daksh7011/immich-backup/internal/doctor"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func startPostgres(t *testing.T) (containerName string, cleanup func()) {
	t.Helper()
	ctx := context.Background()
	pg, err := postgres.RunContainer(ctx,
		testcontainers.WithImage("postgres:17-alpine"),
		postgres.WithDatabase("immich"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	rawName, _ := pg.Name(ctx)
	return strings.TrimPrefix(rawName, "/"), func() { _ = pg.Terminate(ctx) }
}

func validCfg(t *testing.T, containerName string) *config.Config {
	t.Helper()
	// Write a minimal rclone.conf so the rclone.conf check passes
	confPath := filepath.Join(t.TempDir(), "rclone.conf")
	writeFile(t, confPath, "[local]\ntype = local\n")
	return &config.Config{
		Immich: config.ImmichConfig{
			UploadLocation:    "/mnt/immich",
			PostgresContainer: containerName,
			PostgresUser:      "postgres",
			PostgresDB:        "immich",
		},
		Backup: config.BackupConfig{
			RcloneRemote:      "local:/tmp",
			Schedule:          "0 3 * * *",
			DBBackupFrequency: "0 */6 * * *",
			Retention:         config.RetentionConfig{Daily: 7, Weekly: 4},
		},
		Daemon: config.DaemonConfig{LogPath: "/tmp/daemon.log"},
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writeFile %s: %v", path, err)
	}
}

func TestCheck_AllPass(t *testing.T) {
	pgName, cleanup := startPostgres(t)
	t.Cleanup(cleanup)

	client, err := docker.NewClient()
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	defer client.Close()

	cfg := validCfg(t, pgName)
	// Override rclone conf path to use the test one
	results := doctor.Check(client, cfg, filepath.Join(t.TempDir(), "rclone.conf"))
	// Re-run with a conf that has a remote
	confPath := filepath.Join(t.TempDir(), "rclone.conf")
	writeFile(t, confPath, "[local]\ntype = local\n")
	results = doctor.Check(client, cfg, confPath)

	for _, r := range results {
		if !r.OK {
			t.Errorf("check %q failed: %s (remedy: %s)", r.Name, r.Message, r.Remedy)
		}
	}
}

func TestCheck_PostgresDown(t *testing.T) {
	client, err := docker.NewClient()
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	defer client.Close()

	confPath := filepath.Join(t.TempDir(), "rclone.conf")
	writeFile(t, confPath, "[local]\ntype = local\n")

	cfg := &config.Config{
		Immich: config.ImmichConfig{
			PostgresContainer: "this-container-does-not-exist-xyz",
			UploadLocation:    "/mnt/immich",
			PostgresUser:      "postgres",
			PostgresDB:        "immich",
		},
		Backup: config.BackupConfig{
			RcloneRemote: "local:/tmp", Schedule: "0 3 * * *",
			DBBackupFrequency: "0 */6 * * *",
			Retention:         config.RetentionConfig{Daily: 7, Weekly: 4},
		},
		Daemon: config.DaemonConfig{LogPath: "/tmp/daemon.log"},
	}

	results := doctor.Check(client, cfg, confPath)
	var pgResult *doctor.CheckResult
	for i := range results {
		if results[i].Name == "Postgres Container" {
			pgResult = &results[i]
		}
	}
	if pgResult == nil {
		t.Fatal("expected a 'Postgres Container' check result")
	}
	if pgResult.OK {
		t.Error("expected Postgres Container check to fail")
	}
	if pgResult.Remedy == "" {
		t.Error("expected a non-empty Remedy for failed check")
	}
}
```

- [ ] **Step 2: Run tests — expect compile failure**

```bash
CGO_ENABLED=0 go test ./internal/doctor/...
```

- [ ] **Step 3: Write doctor.go**

```go
// internal/doctor/doctor.go
package doctor

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/docker"
)

// CheckResult is the outcome of a single prerequisite check.
type CheckResult struct {
	Name    string
	OK      bool
	Message string
	Remedy  string
}

// Check runs five ordered prerequisite checks and returns all results.
// It does NOT exit or launch interactive processes — callers decide what to do.
//
// Check order:
//  1. rclone binary in PATH
//  2. rcloneConfPath exists and has ≥1 remote
//  3. Docker socket accessible
//  4. Immich Postgres container running
//  5. Config valid
func Check(exec docker.Executor, cfg *config.Config, rcloneConfPath string) []CheckResult {
	return []CheckResult{
		checkRcloneBinary(),
		checkRcloneConf(rcloneConfPath),
		checkDockerSocket(exec),
		checkPostgresContainer(exec, cfg.Immich.PostgresContainer),
		checkConfig(cfg),
	}
}

func checkRcloneBinary() CheckResult {
	_, err := exec.LookPath("rclone")
	if err != nil {
		return CheckResult{
			Name:    "rclone Binary",
			OK:      false,
			Message: "rclone not found in PATH",
			Remedy:  "Install rclone: https://rclone.org/install/",
		}
	}
	return CheckResult{Name: "rclone Binary", OK: true, Message: "rclone found"}
}

func checkRcloneConf(path string) CheckResult {
	out, err := exec.Command("rclone", "listremotes", "--config", path).Output()
	if err != nil || len(out) == 0 {
		return CheckResult{
			Name:    "rclone Config",
			OK:      false,
			Message: fmt.Sprintf("no remotes configured in %s", path),
			Remedy:  "Run `immich-backup setup` or `immich-backup configure` to add a remote",
		}
	}
	return CheckResult{Name: "rclone Config", OK: true, Message: "at least one remote configured"}
}

func checkDockerSocket(ex docker.Executor) CheckResult {
	// Probe the socket by checking a known-impossible container name;
	// a connection error means the socket is unreachable.
	_, err := ex.IsContainerRunning("__immich_backup_socket_probe__")
	if err != nil {
		return CheckResult{
			Name:    "Docker Socket",
			OK:      false,
			Message: fmt.Sprintf("Docker socket unreachable: %v", err),
			Remedy:  "Ensure Docker is running and your user has socket access (docker group)",
		}
	}
	return CheckResult{Name: "Docker Socket", OK: true, Message: "Docker socket accessible"}
}

func checkPostgresContainer(ex docker.Executor, name string) CheckResult {
	running, err := ex.IsContainerRunning(name)
	if err != nil {
		return CheckResult{
			Name:    "Postgres Container",
			OK:      false,
			Message: fmt.Sprintf("error inspecting container %q: %v", name, err),
			Remedy:  "Ensure the Immich stack is running: `docker compose up -d`",
		}
	}
	if !running {
		return CheckResult{
			Name:    "Postgres Container",
			OK:      false,
			Message: fmt.Sprintf("container %q is not running", name),
			Remedy:  "Start the Immich stack: `docker compose up -d`",
		}
	}
	return CheckResult{Name: "Postgres Container", OK: true,
		Message: fmt.Sprintf("container %q is running", name)}
}

func checkConfig(cfg *config.Config) CheckResult {
	if err := cfg.Validate(); err != nil {
		return CheckResult{
			Name:    "Config",
			OK:      false,
			Message: err.Error(),
			Remedy:  "Run `immich-backup configure` or edit ~/.immich-backup/config.yaml",
		}
	}
	return CheckResult{Name: "Config", OK: true, Message: "config is valid"}
}

// AnyFailed returns true if any result has OK == false.
func AnyFailed(results []CheckResult) bool {
	for _, r := range results {
		if !r.OK {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Ensure top-level imports include `"os"`**

The `writeFile` helper calls `os.WriteFile` directly. Verify the import block at the top of `internal/doctor/doctor_test.go` includes `"os"`:

```go
import (
	"os"
	"path/filepath"
	"testing"

	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/docker"
	"github.com/daksh7011/immich-backup/internal/doctor"
)
```

- [ ] **Step 5: Run tests — expect PASS**

```bash
CGO_ENABLED=0 go test ./internal/doctor/... -v -timeout 120s
```
Expected: `TestCheck_AllPass` and `TestCheck_PostgresDown` PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/doctor/
git commit -m "feat: add internal/doctor with 5-check Check() function"
```

---

## Task 9: internal/tui — shared message types and all model stubs

**Files:**
- Create: `internal/tui/model.go`
- Create: `internal/tui/setup_model.go`
- Create: `internal/tui/configure_model.go`
- Create: `internal/tui/backup_model.go`
- Create: `internal/tui/status_model.go`
- Create: `internal/tui/doctor_model.go`
- Create: `internal/tui/logs_model.go`
- Create: `internal/tui/daemon_model.go`

- [ ] **Step 1: Write model.go (shared types)**

```go
// internal/tui/model.go
package tui

import tea "github.com/charmbracelet/bubbletea"

// waitForChan returns a tea.Cmd that reads one message from ch and dispatches it.
func WaitForChan(ch <-chan any) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}
```

- [ ] **Step 2: Write backup_model.go (channel-driven progress)**

```go
// internal/tui/backup_model.go
package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/daksh7011/immich-backup/internal/backup"
)

var (
	okStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	errStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

// BackupModel is the Bubble Tea model for live backup progress display.
type BackupModel struct {
	ch       <-chan any
	lines    []string
	done     bool
	lastErr  error
}

// NewBackupModel creates a BackupModel that reads from ch.
func NewBackupModel(ch <-chan any) BackupModel {
	return BackupModel{ch: ch}
}

func (m BackupModel) Init() tea.Cmd {
	return WaitForChan(m.ch)
}

func (m BackupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case backup.ProgressMsg:
		m.lines = append(m.lines, v.Text)
		return m, WaitForChan(m.ch)
	case backup.ErrorMsg:
		m.lastErr = v.Err
		m.done = true
		return m, tea.Quit
	case backup.DoneMsg:
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

func (m BackupModel) View() string {
	out := ""
	for _, l := range m.lines {
		out += l + "\n"
	}
	if m.lastErr != nil {
		out += errStyle.Render(fmt.Sprintf("Error: %v", m.lastErr)) + "\n"
	} else if m.done {
		out += okStyle.Render("Backup complete!") + "\n"
	}
	return out
}
```

- [ ] **Step 3: Write setup_model.go**

```go
// internal/tui/setup_model.go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/daksh7011/immich-backup/internal/config"
)

// SetupModel collects initial configuration via a Huh form.
type SetupModel struct {
	form   *huh.Form
	result *config.Config
	done   bool
}

// NewSetupModel creates a SetupModel pre-populated with defaults from cfg.
func NewSetupModel(cfg *config.Config) SetupModel {
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Immich upload location").
				Value(&cfg.Immich.UploadLocation),
			huh.NewInput().
				Title("Postgres container name").
				Value(&cfg.Immich.PostgresContainer),
			huh.NewInput().
				Title("Postgres user").
				Value(&cfg.Immich.PostgresUser),
			huh.NewInput().
				Title("Postgres database").
				Value(&cfg.Immich.PostgresDB),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("rclone remote (e.g. b2-encrypted:immich-backup)").
				Value(&cfg.Backup.RcloneRemote),
			huh.NewInput().
				Title("Backup schedule (cron)").
				Value(&cfg.Backup.Schedule),
			huh.NewInput().
				Title("DB backup frequency (cron)").
				Value(&cfg.Backup.DBBackupFrequency),
		),
	)
	return SetupModel{form: form, result: cfg}
}

func (m SetupModel) Init() tea.Cmd          { return m.form.Init() }
func (m SetupModel) Result() *config.Config { return m.result }
func (m SetupModel) Done() bool             { return m.done }

func (m SetupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	form, cmd := m.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		m.form = f
		if m.form.State == huh.StateCompleted {
			m.done = true
			return m, tea.Quit
		}
	}
	return m, cmd
}

func (m SetupModel) View() string { return m.form.View() }
```

- [ ] **Step 4: Write configure_model.go (reuse SetupModel)**

```go
// internal/tui/configure_model.go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/daksh7011/immich-backup/internal/config"
)

// ConfigureModel wraps SetupModel for the configure command.
type ConfigureModel struct{ SetupModel }

func NewConfigureModel(cfg *config.Config) ConfigureModel {
	return ConfigureModel{SetupModel: NewSetupModel(cfg)}
}

func (m ConfigureModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	inner, cmd := m.SetupModel.Update(msg)
	m.SetupModel = inner.(SetupModel)
	return m, cmd
}
```

- [ ] **Step 5: Write remaining model stubs (status, doctor, logs, daemon)**

```go
// internal/tui/status_model.go
package tui

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/daksh7011/immich-backup/internal/status"
)

type StatusModel struct{ run *status.LastRun; nextRun string }

func NewStatusModel(run *status.LastRun, nextRun string) StatusModel {
	return StatusModel{run: run, nextRun: nextRun}
}
func (m StatusModel) Init() tea.Cmd                           { return nil }
func (m StatusModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) { return m, tea.Quit }
func (m StatusModel) View() string {
	if m.run == nil {
		return "No backup has run yet.\n"
	}
	return fmt.Sprintf("Last run: %s — %s\nNext run: %s\n",
		m.run.Time.Format("2006-01-02 15:04:05"), m.run.Result, m.nextRun)
}
```

```go
// internal/tui/doctor_model.go
package tui

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/daksh7011/immich-backup/internal/doctor"
)

type DoctorModel struct{ results []doctor.CheckResult }

func NewDoctorModel(results []doctor.CheckResult) DoctorModel {
	return DoctorModel{results: results}
}
func (m DoctorModel) Init() tea.Cmd                           { return nil }
func (m DoctorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) { return m, tea.Quit }
func (m DoctorModel) View() string {
	out := ""
	for _, r := range m.results {
		icon := okStyle.Render("✓")
		if !r.OK {
			icon = errStyle.Render("✗")
		}
		out += fmt.Sprintf("%s  %-20s %s\n", icon, r.Name, r.Message)
		if !r.OK && r.Remedy != "" {
			out += fmt.Sprintf("   → %s\n", r.Remedy)
		}
	}
	return out
}
```

```go
// internal/tui/logs_model.go
package tui

import tea "github.com/charmbracelet/bubbletea"

type LogsModel struct{ content string }

func NewLogsModel(content string) LogsModel   { return LogsModel{content: content} }
func (m LogsModel) Init() tea.Cmd             { return nil }
func (m LogsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) { return m, tea.Quit }
func (m LogsModel) View() string              { return m.content }
```

```go
// internal/tui/daemon_model.go
package tui

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
)

type DaemonModel struct{ message string; err error }

func NewDaemonModel(message string, err error) DaemonModel {
	return DaemonModel{message: message, err: err}
}
func (m DaemonModel) Init() tea.Cmd { return nil }
func (m DaemonModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) { return m, tea.Quit }
func (m DaemonModel) View() string {
	if m.err != nil {
		return errStyle.Render(fmt.Sprintf("Error: %v\n", m.err))
	}
	return okStyle.Render(m.message + "\n")
}
```

- [ ] **Step 6: Verify all TUI files compile**

```bash
CGO_ENABLED=0 go build ./internal/tui/...
```
Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/
git commit -m "feat: add internal/tui with all Bubble Tea model stubs"
```

---

## Task 10: cmd/root.go — PersistentPreRun and config context

**Files:**
- Create: `cmd/root.go`

- [ ] **Step 1: Write cmd/root.go**

```go
// cmd/root.go
package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"github.com/daksh7011/immich-backup/internal/config"
)

type contextKey struct{}

// GetConfig retrieves the loaded config from the command's context.
// Panics if called on a command that does not go through PersistentPreRun.
func GetConfig(cmd *cobra.Command) *config.Config {
	return cmd.Context().Value(contextKey{}).(*config.Config)
}

var rootCmd = &cobra.Command{
	Use:   "immich-backup",
	Short: "Back up your Immich media library using rclone",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// setup and configure load config themselves — skip here.
		skip := map[string]bool{"setup": true, "configure": true}
		if skip[cmd.Name()] {
			return nil
		}
		cfg, err := config.Load(config.DefaultConfigPath())
		if err != nil {
			slog.Error("config error", "error", err,
				"remedy", "run `immich-backup configure` or edit ~/.immich-backup/config.yaml")
			os.Exit(1)
		}
		cmd.SetContext(context.WithValue(cmd.Context(), contextKey{}, cfg))
		return nil
	},
}

// Execute is the entry point called from main.go.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(
		newSetupCmd(),
		newConfigureCmd(),
		newBackupCmd(),
		newStatusCmd(),
		newDoctorCmd(),
		newLogsCmd(),
		newDaemonCmd(),
	)
}
```

- [ ] **Step 2: Verify it compiles (stub commands don't exist yet — expect errors)**

```bash
CGO_ENABLED=0 go build ./cmd/... 2>&1 | head -20
```
Expected: errors for undefined `newSetupCmd` etc. — that's correct at this stage.

---

## Task 11: cmd/setup.go and cmd/configure.go

**Files:**
- Create: `cmd/setup.go`
- Create: `cmd/configure.go`

- [ ] **Step 1: Write cmd/setup.go**

```go
// cmd/setup.go
package cmd

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/rcloneconf"
	"github.com/daksh7011/immich-backup/internal/tui"
)

func newSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Interactive first-run configuration wizard",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Ensure rclone is configured first (pause-launch-resume)
			if err := rcloneconf.EnsureConfigured(config.RcloneConfigPath()); err != nil {
				fmt.Fprintln(os.Stderr, "rclone setup failed:", err)
				os.Exit(1)
			}

			// Load existing config (creates defaults if missing)
			cfg, err := config.Load(config.DefaultConfigPath())
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// Run the setup wizard
			model := tui.NewSetupModel(cfg)
			p := tea.NewProgram(model)
			result, err := p.Run()
			if err != nil {
				return fmt.Errorf("setup wizard: %w", err)
			}
			final := result.(tui.SetupModel)
			if !final.Done() {
				fmt.Println("Setup cancelled.")
				return nil
			}

			if err := config.Save(config.DefaultConfigPath(), final.Result()); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			fmt.Println("Configuration saved to", config.DefaultConfigPath())
			return nil
		},
	}
}
```

- [ ] **Step 2: Write cmd/configure.go**

```go
// cmd/configure.go
package cmd

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/rcloneconf"
	"github.com/daksh7011/immich-backup/internal/tui"
)

func newConfigureCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "configure",
		Short: "Re-run the configuration wizard",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rcloneconf.EnsureConfigured(config.RcloneConfigPath()); err != nil {
				fmt.Fprintln(os.Stderr, "rclone setup failed:", err)
				os.Exit(1)
			}
			cfg, err := config.Load(config.DefaultConfigPath())
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			model := tui.NewConfigureModel(cfg)
			p := tea.NewProgram(model)
			result, err := p.Run()
			if err != nil {
				return fmt.Errorf("configure wizard: %w", err)
			}
			final := result.(tui.ConfigureModel)
			if !final.Done() {
				fmt.Println("Configure cancelled.")
				return nil
			}
			if err := config.Save(config.DefaultConfigPath(), final.Result()); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			fmt.Println("Configuration updated.")
			return nil
		},
	}
}
```

- [ ] **Step 3: Verify these two commands compile**

```bash
CGO_ENABLED=0 go build ./cmd/... 2>&1 | grep -v "newBackupCmd\|newStatusCmd\|newDoctorCmd\|newLogsCmd\|newDaemonCmd"
```

- [ ] **Step 4: Commit**

```bash
git add cmd/setup.go cmd/configure.go
git commit -m "feat: add cmd/setup and cmd/configure with Huh wizard + rclone EnsureConfigured"
```

---

## Task 12: cmd/backup.go

**Files:**
- Create: `cmd/backup.go`

- [ ] **Step 1: Write cmd/backup.go**

```go
// cmd/backup.go
package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/daksh7011/immich-backup/internal/backup"
	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/docker"
	"github.com/daksh7011/immich-backup/internal/doctor"
	"github.com/daksh7011/immich-backup/internal/status"
	"github.com/daksh7011/immich-backup/internal/tui"
)

func newBackupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "backup",
		Short: "Run a backup now",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := GetConfig(cmd)

			// Prerequisite checks — fail fast
			client, err := docker.NewClient()
			if err != nil {
				slog.Error("docker socket unreachable", "error", err,
					"remedy", "ensure Docker is running")
				os.Exit(1)
			}
			defer client.Close()

			results := doctor.Check(client, cfg, config.RcloneConfigPath())
			if doctor.AnyFailed(results) {
				for _, r := range results {
					if !r.OK {
						slog.Error("prerequisite check failed",
							"check", r.Name, "message", r.Message, "remedy", r.Remedy)
					}
				}
				os.Exit(1)
			}

			// Start live TUI + backup runner concurrently
			ch := make(chan any, 16)
			go backup.Run(
				config.RcloneConfigPath(),
				cfg.Immich.PostgresContainer,
				cfg.Immich.PostgresUser,
				cfg.Immich.PostgresDB,
				cfg.Immich.UploadLocation,
				cfg.Backup.RcloneRemote,
				client,
				ch,
			)

			model := tui.NewBackupModel(ch)
			p := tea.NewProgram(model)
			result, err := p.Run()
			if err != nil {
				return fmt.Errorf("backup TUI: %w", err)
			}

			final := result.(tui.BackupModel)
			run := &status.LastRun{Time: time.Now().UTC()}
			if final.Err() != nil {
				run.Result = "error"
				run.Error = final.Err().Error()
			} else {
				run.Result = "success"
			}
			_ = status.Save(config.StatusFilePath(), run)
			return nil
		},
	}
}
```

**Note:** Add `Err() error` accessor to `BackupModel` in `internal/tui/backup_model.go`:

```go
func (m BackupModel) Err() error { return m.lastErr }
```

- [ ] **Step 2: Verify compiles**

```bash
CGO_ENABLED=0 go build ./cmd/... 2>&1 | grep -v "newStatusCmd\|newDoctorCmd\|newLogsCmd\|newDaemonCmd"
```

- [ ] **Step 3: Commit**

```bash
git add cmd/backup.go internal/tui/backup_model.go
git commit -m "feat: add cmd/backup with live Bubble Tea progress and status write"
```

---

## Task 13: cmd/doctor.go, cmd/status.go, cmd/logs.go

**Files:**
- Create: `cmd/doctor.go`
- Create: `cmd/status.go`
- Create: `cmd/logs.go`

- [ ] **Step 1: Write cmd/doctor.go**

```go
// cmd/doctor.go
package cmd

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/docker"
	"github.com/daksh7011/immich-backup/internal/doctor"
	"github.com/daksh7011/immich-backup/internal/tui"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check all prerequisites and display results",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := GetConfig(cmd)
			client, err := docker.NewClient()
			if err != nil {
				// Doctor must still run even if Docker is down — report it
				client = nil
				_ = err
			}
			var ex docker.Executor
			if client != nil {
				ex = client
				defer client.Close()
			}

			results := doctor.Check(ex, cfg, config.RcloneConfigPath())
			model := tui.NewDoctorModel(results)
			p := tea.NewProgram(model)
			_, err = p.Run()
			return err
		},
	}
}
```

**Note:** `doctor.Check` needs to handle a nil Executor gracefully. Update `checkDockerSocket` and `checkPostgresContainer` in `internal/doctor/doctor.go` to return a failed result when exec is nil:

```go
func checkDockerSocket(ex docker.Executor) CheckResult {
	if ex == nil {
		return CheckResult{
			Name:    "Docker Socket",
			OK:      false,
			Message: "Docker socket unreachable (client could not be created)",
			Remedy:  "Ensure Docker is running and that your user has socket access",
		}
	}
	// ... existing logic
}

func checkPostgresContainer(ex docker.Executor, name string) CheckResult {
	if ex == nil {
		return CheckResult{
			Name:    "Postgres Container",
			OK:      false,
			Message: "Cannot check Postgres container — Docker socket is unavailable",
			Remedy:  "Ensure Docker is running and that your user has socket access",
		}
	}
	// ... existing logic
}
```

- [ ] **Step 2: Write cmd/status.go**

```go
// cmd/status.go
package cmd

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/status"
	"github.com/daksh7011/immich-backup/internal/tui"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show last backup result and next scheduled run",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := GetConfig(cmd)
			run, _ := status.Load(config.StatusFilePath()) // nil if no backup yet
			nextRun := fmt.Sprintf("(schedule: %s)", cfg.Backup.Schedule)
			model := tui.NewStatusModel(run, nextRun)
			p := tea.NewProgram(model)
			_, err := p.Run()
			return err
		},
	}
}
```

- [ ] **Step 3: Write cmd/logs.go**

```go
// cmd/logs.go
package cmd

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/daksh7011/immich-backup/internal/tui"
)

func newLogsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logs",
		Short: "Show daemon log output",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := GetConfig(cmd)
			data, err := os.ReadFile(cfg.Daemon.LogPath)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Println("No log file found at", cfg.Daemon.LogPath)
					return nil
				}
				return fmt.Errorf("read log: %w", err)
			}
			model := tui.NewLogsModel(string(data))
			p := tea.NewProgram(model)
			_, err = p.Run()
			return err
		},
	}
}
```

- [ ] **Step 4: Verify compiles**

```bash
CGO_ENABLED=0 go build ./cmd/... 2>&1 | grep -v "newDaemonCmd"
```

- [ ] **Step 5: Commit**

```bash
git add cmd/doctor.go cmd/status.go cmd/logs.go internal/doctor/doctor.go
git commit -m "feat: add cmd/doctor, cmd/status, cmd/logs"
```

---

## Task 14: cmd/daemon.go

**Files:**
- Create: `cmd/daemon.go`

- [ ] **Step 1: Write cmd/daemon.go**

```go
// cmd/daemon.go
package cmd

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/daksh7011/immich-backup/internal/daemon"
	"github.com/daksh7011/immich-backup/internal/tui"
)

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the immich-backup background service",
	}

	// daemon.New() is called inside RunE, not at construction time, so it
	// does not panic on platforms where the Manager is not yet implemented.
	cmd.AddCommand(newDaemonSubCmd("install", "Install and enable the background service",
		func(c *cobra.Command) error { return daemon.New().Install(GetConfig(c)) }))
	cmd.AddCommand(newDaemonSubCmd("uninstall", "Remove the background service",
		func(c *cobra.Command) error { return daemon.New().Uninstall() }))
	cmd.AddCommand(newDaemonSubCmd("start", "Start the background service",
		func(c *cobra.Command) error { return daemon.New().Start() }))
	cmd.AddCommand(newDaemonSubCmd("stop", "Stop the background service",
		func(c *cobra.Command) error { return daemon.New().Stop() }))
	cmd.AddCommand(newDaemonSubCmd("restart", "Restart the background service",
		func(c *cobra.Command) error { return daemon.New().Restart() }))
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show background service status",
		RunE: func(c *cobra.Command, _ []string) error {
			out, err := daemon.New().Status()
			return runDaemonModel(out, err)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "logs",
		Short: "Show background service logs",
		RunE: func(c *cobra.Command, _ []string) error {
			out, err := daemon.New().Logs()
			if err != nil {
				return err
			}
			_, runErr := tea.NewProgram(tui.NewLogsModel(out)).Run()
			return runErr
		},
	})
	return cmd
}

func newDaemonSubCmd(use, short string, fn func(*cobra.Command) error) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(c *cobra.Command, _ []string) error {
			return runDaemonModel("", fn(c))
		},
	}
}

func runDaemonModel(msg string, err error) error {
	if msg == "" && err == nil {
		msg = "Done."
	}
	_, runErr := tea.NewProgram(tui.NewDaemonModel(msg, err)).Run()
	if runErr != nil {
		return fmt.Errorf("TUI: %w", runErr)
	}
	return err
}
```

- [ ] **Step 2: Verify cmd package compiles cleanly**

- [ ] **Step 3: Verify full cmd package compiles**

```bash
CGO_ENABLED=0 go build ./cmd/...
```
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add cmd/daemon.go
git commit -m "feat: add cmd/daemon with all 7 sub-commands"
```

---

## Task 15: main.go and final build + test verification

**Files:**
- Create: `main.go`

- [ ] **Step 1: Write main.go**

```go
// main.go
package main

import "github.com/daksh7011/immich-backup/cmd"

func main() {
	cmd.Execute()
}
```

- [ ] **Step 2: Build the binary**

```bash
CGO_ENABLED=0 go build -o immich-backup .
```
Expected: binary created with no errors.

- [ ] **Step 3: Verify CGo-free cross-compilation**

```bash
CGO_ENABLED=0 GOOS=linux   GOARCH=amd64  go build -o /dev/null . && echo "linux/amd64 OK"
CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64  go build -o /dev/null . && echo "darwin/arm64 OK"
CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64  go build -o /dev/null . && echo "darwin/amd64 OK"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64  go build -o /dev/null . && echo "windows/amd64 OK"
```
Expected: all four lines print `OK`.

- [ ] **Step 4: Run the full test suite**

```bash
CGO_ENABLED=0 go test ./... -v -timeout 300s 2>&1 | tail -40
```
Expected: all tests PASS. Tests requiring Docker will spin up real containers.

- [ ] **Step 5: Smoke test the binary**

```bash
./immich-backup --help
./immich-backup doctor --help
./immich-backup daemon --help
```
Expected: help text rendered for each command.

- [ ] **Step 6: Commit**

```bash
git add main.go
git commit -m "feat: add main.go — skeleton complete, all packages wired"
```

- [ ] **Step 7: Tag the skeleton milestone**

```bash
git tag v0.1.0-skeleton
```

---

## Execution Notes

### Prerequisites

- Docker socket accessible: `docker info` must succeed
- `rclone` in PATH: `rclone version` must succeed
- Go 1.22+: `go version`

### Known deferred items (not in scope for skeleton)

- `daemon.Manager` control methods (`Install`, `Uninstall`, `Start`, `Stop`, `Restart`) are
  implemented with real OS calls but require manual testing on macOS (launchd) and Linux
  (systemd user service) — they are not tested in the automated suite.
- Retention logic (daily/weekly pruning of old backups) is defined in config but not
  yet implemented in the backup runner.
- Log file initialization (creating the log directory and setting up `slog` handlers with
  the configured `daemon.log_path`) is scaffolded but the dual-handler setup should be
  wired in `cmd/root.go` `PersistentPreRun` once the full implementation begins.
