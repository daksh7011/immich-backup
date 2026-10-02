package daemon

import "time"

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
	return &systemdManager{run: r, geteuid: geteuid, getenv: getenv, username: username,
		unitPath: unitPath, timerPath: timerPath}
}

// Activate exposes the post-write systemctl and linger sequence of Install.
func (m *systemdManager) Activate() error { return m.activate() }

// Preflight exposes the root and XDG_RUNTIME_DIR checks.
func (m *systemdManager) Preflight() error { return m.preflight() }

// ParseLinger exposes parseLinger.
var ParseLinger = parseLinger

// LaunchdManager exposes launchdManager so tests can drive it with a fake
// command runner.
type LaunchdManager = launchdManager

// NewLaunchdManagerWith builds a launchd manager with an injected command
// runner, uid and plist path.
func NewLaunchdManagerWith(r runner, getuid func() int, plistPath func() string) *LaunchdManager {
	return &launchdManager{run: r, getuid: getuid, plistPath: plistPath, sleep: func(time.Duration) {}, now: time.Now}
}

// Activate exposes the bootout, enable, bootstrap and verify sequence of
// Install.
func (m *launchdManager) Activate() error { return m.activate() }

// SetPaths points the systemd manager at test unit and timer files.
func (m *systemdManager) SetPaths(unit, timer string) {
	m.unitPath = func() string { return unit }
	m.timerPath = func() string { return timer }
}

// SetNow fixes the clock the launchd manager computes the next run from.
func (m *launchdManager) SetNow(now time.Time) { m.now = func() time.Time { return now } }

// State parsing and rendering helpers.
var (
	ParseSystemctlShow  = parseSystemctlShow
	ParseLaunchctlPrint = parseLaunchctlPrint
	SystemdState        = systemdState
	LaunchdState        = launchdState
	ParseSystemdUnit    = parseSystemdUnit
	ParsePlist          = parsePlist
	NextDailyRun        = nextDailyRun
	SystemdTime         = systemdTime
	Report              = report
)

// PlistInfo exposes plistInfo for LaunchdState.
type PlistInfo = plistInfo

// NeverExited exposes neverExited.
const NeverExited = neverExited
