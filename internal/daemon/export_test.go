package daemon

// Test-only hooks for the external daemon_test package.

// ResolveServiceEnvWith exposes resolveServiceEnv with injectable rclone
// resolution and environment lookup.
var ResolveServiceEnvWith = resolveServiceEnv

// StableExecutableWith exposes stableExecutable with injectable PATH lookup,
// file comparison and temp dir.
var StableExecutableWith = stableExecutable

// SameFile exposes the default file comparison used by StableExecutable.
var SameFile = sameFile

// CheckUnitPath exposes checkUnitPath.
var CheckUnitPath = checkUnitPath

// SystemdManager exposes systemdManager so tests can drive it with a fake
// command runner.
type SystemdManager = systemdManager

// NewSystemdManagerWith builds a systemd manager with an injected command
// runner, euid, environment lookup and user name.
func NewSystemdManagerWith(r runner, geteuid func() int, getenv func(string) string, username func() (string, error)) *SystemdManager {
	return &systemdManager{run: r, geteuid: geteuid, getenv: getenv, username: username}
}

// Activate exposes the post-write systemctl and linger sequence of Install.
func (m *systemdManager) Activate() error { return m.activate() }

// Preflight exposes the root and XDG_RUNTIME_DIR checks.
func (m *systemdManager) Preflight() error { return m.preflight() }

// ParseLinger exposes parseLinger.
var ParseLinger = parseLinger
