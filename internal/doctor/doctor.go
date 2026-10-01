// internal/doctor/doctor.go
package doctor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/daemon"
	"github.com/daksh7011/immich-backup/internal/docker"
	"github.com/daksh7011/immich-backup/internal/rclonebin"
)

// CheckStartMsg is sent on the channel immediately before each check begins.
// Consumers can use it to show a spinner for the named check.
type CheckStartMsg struct{ Name string }

// CheckResult is the outcome of a single prerequisite check.
type CheckResult struct {
	Name    string
	OK      bool
	Warn    bool // with OK false: a problem worth fixing that does not block a backup
	Message string
	Remedy  string
}

// Check runs five ordered prerequisite checks and returns all results.
// It does NOT exit or launch interactive processes — callers decide what to do.
//
// Check order:
//  1. rclone binary in PATH or a well-known install dir
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
	path, err := rclonebin.Resolve()
	if err != nil {
		return CheckResult{
			Name:    "rclone Binary",
			OK:      false,
			Message: err.Error(),
			Remedy:  "Install rclone: https://rclone.org/install/",
		}
	}
	return CheckResult{Name: "rclone Binary", OK: true, Message: fmt.Sprintf("rclone found at %s", path)}
}

func checkRcloneConf(path string) CheckResult {
	out, err := exec.Command(rclonebin.Path(), "listremotes", "--config", path).Output()
	return rcloneConfResult(path, out, err)
}

// rcloneConfResult turns `rclone listremotes` output into a check result.
// When rclone itself fails (e.g. an encrypted config with no password), its
// stderr is reported instead of a misleading "no remotes configured".
func rcloneConfResult(path string, out []byte, err error) CheckResult {
	if err != nil {
		msg := err.Error()
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(bytes.TrimSpace(ee.Stderr)) > 0 {
			msg = strings.TrimSpace(string(ee.Stderr))
		}
		return CheckResult{
			Name:    "rclone Config",
			OK:      false,
			Message: fmt.Sprintf("rclone listremotes --config %s failed: %s", path, msg),
			Remedy:  "Fix the rclone config, or run `immich-backup configure` to recreate the remote",
		}
	}
	if len(bytes.TrimSpace(out)) == 0 {
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
			Message: fmt.Sprintf("Docker socket unreachable at %s (client could not be created)", docker.Host()),
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
			Message: fmt.Sprintf("Docker socket unreachable at %s: %v", docker.Host(), err),
			Remedy: "Ensure Docker is running and your user has socket access (docker group). " +
				"For rootless Docker, Colima or Podman, export DOCKER_HOST " +
				"(e.g. unix://$XDG_RUNTIME_DIR/docker.sock) and re-run `immich-backup daemon install`",
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

// checkConfigLoad reports loadErr (a parse or validation error from
// config.Load) as the failed Config check, so doctor names the real problem
// instead of validating the empty fallback config.
func checkConfigLoad(cfg *config.Config, loadErr error) CheckResult {
	if loadErr != nil {
		return CheckResult{
			Name:    "Config",
			OK:      false,
			Message: fmt.Sprintf("config load failed: %v", loadErr),
			Remedy:  "Run `immich-backup configure` or edit ~/.immich-backup/config.yaml",
		}
	}
	return checkConfig(cfg)
}

// Service is the part of daemon.Manager the background service checks use.
type Service interface {
	State() (daemon.State, error)
	Definition() (*daemon.Definition, error)
}

type namedCheck struct {
	name string
	fn   func() CheckResult
}

// serviceChecks returns the background service checks for goos. They only
// warn: a backup run by hand works without the service, so they never make
// AnyFailed true. svc is nil where the platform has no service manager.
func serviceChecks(svc Service, goos string) []namedCheck {
	checks := []namedCheck{{"Daemon Program", func() CheckResult { return checkServiceProgram(svc, goos) }}}
	if goos == "linux" && svc != nil {
		checks = append(checks, namedCheck{"Daemon Linger", func() CheckResult { return checkServiceLinger(svc) }})
	}
	return checks
}

// checkServiceProgram checks that the installed unit or plist runs a program
// that still exists; after a move or an upgrade that removed it, every
// scheduled run fails before the backup starts (systemd 203/EXEC).
func checkServiceProgram(svc Service, goos string) CheckResult {
	const name = "Daemon Program"
	if svc == nil {
		return CheckResult{Name: name, Warn: true,
			Message: fmt.Sprintf("the background service is not supported on %s; schedule `immich-backup backup` yourself", goos)}
	}
	def, err := svc.Definition()
	if err != nil {
		return CheckResult{Name: name, Warn: true, Message: err.Error(),
			Remedy: "Re-run `immich-backup daemon install`"}
	}
	if def == nil {
		return CheckResult{Name: name, Warn: true,
			Message: "the background service is not installed, so no backups are scheduled",
			Remedy:  "Run `immich-backup daemon install`"}
	}
	if def.BinaryPath == "" {
		return CheckResult{Name: name, Warn: true,
			Message: fmt.Sprintf("cannot find the program in %s", def.Path),
			Remedy:  "Re-run `immich-backup daemon install`"}
	}
	if err := daemon.CheckProgram(def.BinaryPath); err != nil {
		return CheckResult{Name: name, Warn: true,
			Message: fmt.Sprintf("the installed service runs %s: %v", def.BinaryPath, err),
			Remedy:  "Re-run `immich-backup daemon install` (needed after moving or upgrading immich-backup)"}
	}
	return CheckResult{Name: name, OK: true, Message: fmt.Sprintf("the service runs %s", def.BinaryPath)}
}

// checkServiceLinger checks that systemd keeps the user's timer running
// after logout, which a headless server depends on.
func checkServiceLinger(svc Service) CheckResult {
	const name = "Daemon Linger"
	st, err := svc.State()
	if err != nil {
		return CheckResult{Name: name, Warn: true, Message: fmt.Sprintf("cannot query systemd: %v", err)}
	}
	if st.Linger == "yes" {
		return CheckResult{Name: name, OK: true, Message: "lingering is on: the timer runs while you are logged out"}
	}
	return CheckResult{Name: name, Warn: true,
		Message: fmt.Sprintf("lingering is %s for user %q: systemd stops the backup timer when you log out", st.Linger, st.User),
		Remedy:  fmt.Sprintf("Run `sudo loginctl enable-linger %s`", st.User)}
}

// CheckAsync runs the same five checks as Check, then the non-blocking
// background service checks for svc (nil when the platform has none), and
// streams progress via ch.
// A non-nil cfgErr (from config.Load) is reported as the Config check result.
// For each check it sends CheckStartMsg{Name} then CheckResult.
// The caller is responsible for closing ch after CheckAsync returns.
// ctx cancellation stops further checks and channel sends, preventing a goroutine
// leak when the TUI exits early (e.g. Ctrl+C) before all checks complete.
// Note: an in-progress check function itself is not interrupted by ctx — only
// the sends between checks are guarded.
func CheckAsync(ctx context.Context, ex docker.Executor, cfg *config.Config, cfgErr error, rcloneConfPath string, svc Service, ch chan<- any) {
	checks := []namedCheck{
		{"rclone Binary", checkRcloneBinary},
		{"rclone Config", func() CheckResult { return checkRcloneConf(rcloneConfPath) }},
		{"Docker Socket", func() CheckResult { return checkDockerSocket(ex) }},
		{"Postgres Container", func() CheckResult { return checkPostgresContainer(ex, cfg.Immich.PostgresContainer) }},
		{"Config", func() CheckResult { return checkConfigLoad(cfg, cfgErr) }},
	}
	checks = append(checks, serviceChecks(svc, runtime.GOOS)...)
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

// AnyFailed returns true if any result failed; warnings do not count.
func AnyFailed(results []CheckResult) bool {
	for _, r := range results {
		if !r.OK && !r.Warn {
			return true
		}
	}
	return false
}
