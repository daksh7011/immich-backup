// internal/doctor/doctor.go
package doctor

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/docker"
)

// CheckStartMsg is sent on the channel immediately before each check begins.
// Consumers can use it to show a spinner for the named check.
type CheckStartMsg struct{ Name string }

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
func Check(ex docker.Executor, cfg *config.Config, rcloneConfPath string) []CheckResult {
	return []CheckResult{
		checkRcloneBinary(),
		checkRcloneConf(rcloneConfPath),
		checkDockerSocket(ex),
		checkPostgresContainer(ex, cfg.Immich.PostgresContainer),
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
	if ex == nil {
		return CheckResult{
			Name:    "Docker Socket",
			OK:      false,
			Message: "Docker socket unreachable (client could not be created)",
			Remedy:  "Ensure Docker is running and that your user has socket access",
		}
	}
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
	if ex == nil {
		return CheckResult{
			Name:    "Postgres Container",
			OK:      false,
			Message: "Cannot check Postgres container — Docker socket is unavailable",
			Remedy:  "Ensure Docker is running and that your user has socket access",
		}
	}
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

// CheckAsync runs the same five checks as Check but streams progress via ch.
// For each check it sends CheckStartMsg{Name} then CheckResult.
// The caller is responsible for closing ch after CheckAsync returns.
// ctx cancellation stops further checks and channel sends, preventing a goroutine
// leak when the TUI exits early (e.g. Ctrl+C) before all checks complete.
// Note: an in-progress check function itself is not interrupted by ctx — only
// the sends between checks are guarded.
func CheckAsync(ctx context.Context, ex docker.Executor, cfg *config.Config, rcloneConfPath string, ch chan<- any) {
	type namedCheck struct {
		name string
		fn   func() CheckResult
	}
	checks := []namedCheck{
		{"rclone Binary", checkRcloneBinary},
		{"rclone Config", func() CheckResult { return checkRcloneConf(rcloneConfPath) }},
		{"Docker Socket", func() CheckResult { return checkDockerSocket(ex) }},
		{"Postgres Container", func() CheckResult { return checkPostgresContainer(ex, cfg.Immich.PostgresContainer) }},
		{"Config", func() CheckResult { return checkConfig(cfg) }},
	}
	for _, c := range checks {
		select {
		case ch <- CheckStartMsg{Name: c.name}:
		case <-ctx.Done():
			return
		}
		result := c.fn()
		select {
		case ch <- result:
		case <-ctx.Done():
			return
		}
	}
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
