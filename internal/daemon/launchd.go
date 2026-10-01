// internal/daemon/launchd.go
package daemon

import (
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/daksh7011/immich-backup/internal/config"
)

const plistLabel = "com.immich-backup.agent"
const plistFilename = plistLabel + ".plist"

var plistTmpl = template.Must(template.New("plist").Funcs(template.FuncMap{"xml": xmlEscape}).Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
    "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{xml .Label}}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{xml .BinaryPath}}</string>
        <string>backup</string>
    </array>
    <key>StartCalendarInterval</key>
    <dict>
        <key>Hour</key>
        <integer>{{xml .Hour}}</integer>
        <key>Minute</key>
        <integer>{{xml .Minute}}</integer>
    </dict>
    <key>StandardOutPath</key>
    <string>{{xml .LogPath}}</string>
    <key>StandardErrorPath</key>
    <string>{{xml .LogPath}}</string>
    <key>RunAtLoad</key>
    <false/>
{{- if .Env}}
    <key>EnvironmentVariables</key>
    <dict>
{{- range .Env}}
        <key>{{xml .Key}}</key>
        <string>{{xml .Value}}</string>
{{- end}}
    </dict>
{{- end}}
</dict>
</plist>
`))

// GeneratePlist returns the launchd plist XML for the given binary path and config.
// Returns an error if the schedule uses step, range, or list expressions in the
// minute or hour fields — launchd requires plain integers in StartCalendarInterval.
// env becomes the EnvironmentVariables dict, since launchd gives jobs only
// PATH=/usr/bin:/bin:/usr/sbin:/sbin. Every value is XML-escaped, so paths
// containing & or < still produce a valid plist.
// Exported for testing.
func GeneratePlist(binaryPath string, cfg *config.Config, env ServiceEnv) (string, error) {
	parts := strings.Fields(cfg.Backup.Schedule)
	if len(parts) != 5 {
		return "", fmt.Errorf("schedule must have exactly 5 cron fields, got %d", len(parts))
	}
	minute, hour := parts[0], parts[1]
	if !isSimpleInt(minute) || !isSimpleInt(hour) {
		return "", fmt.Errorf(
			"launchd scheduling only supports simple hour/minute values (e.g. \"0 3 * * *\"); "+
				"step/range/list expressions like %q are not supported — use a specific time",
			cfg.Backup.Schedule,
		)
	}

	var buf strings.Builder
	if err := plistTmpl.Execute(&buf, map[string]any{
		"Label":      plistLabel,
		"BinaryPath": binaryPath,
		"Hour":       hour,
		"Minute":     minute,
		"LogPath":    cfg.Daemon.LogPath,
		"Env":        env.vars(),
	}); err != nil {
		return "", fmt.Errorf("render plist template: %w", err)
	}
	return buf.String(), nil
}

// xmlEscape escapes s for use as plist element text.
func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", plistFilename)
}

// bootoutTimeout bounds the wait for a running job to unload after bootout
// returns "Operation now in progress". launchd sends SIGTERM and escalates to
// SIGKILL after the job's ExitTimeOut (20s by default).
const (
	bootoutTimeout = 30 * time.Second
	bootoutPoll    = time.Second
)

type launchdManager struct {
	run       runner
	getuid    func() int
	plistPath func() string
	sleep     func(time.Duration)
	now       func() time.Time
}

func newLaunchdManager() *launchdManager {
	return &launchdManager{run: execRunner{}, getuid: os.Getuid, plistPath: plistPath, sleep: time.Sleep, now: time.Now}
}

// domain is the per-user GUI launchd domain the agent lives in. The legacy
// load/unload subcommands guess the domain from the caller's session, which
// over SSH is not the GUI one, and report failures only on stderr.
func (m *launchdManager) domain() string { return fmt.Sprintf("gui/%d", m.getuid()) }

func (m *launchdManager) target() string { return m.domain() + "/" + plistLabel }

func (m *launchdManager) launchctl(args ...string) ([]byte, error) {
	out, err := m.run.Run("launchctl", args...)
	if err != nil {
		return out, cmdError("launchctl", args, out, err)
	}
	return out, nil
}

// exitCode returns the exit status of a failed command, or -1 when err does
// not carry one.
func exitCode(err error) int {
	var exit interface{ ExitCode() int }
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}

// notLoaded reports whether a failed launchctl call only means the service
// is not in the domain: exit 3 (ESRCH, "No such process") or 113 ("Could not
// find specified service").
func notLoaded(out []byte, err error) bool {
	if code := exitCode(err); code == 3 || code == 113 {
		return true
	}
	msg := string(out)
	return strings.Contains(msg, "No such process") || strings.Contains(msg, "Could not find")
}

// checkUser rejects root: as root launchctl would act on gui/0, not on the
// session of the user that owns the backups.
func (m *launchdManager) checkUser() error {
	if m.getuid() == 0 {
		return fmt.Errorf("refusing to manage the LaunchAgent as root: " +
			"run immich-backup without sudo, as the user that should own the backups")
	}
	return nil
}

// bootoutInProgress reports whether bootout failed only because launchd is
// still stopping the running job: exit 36 (EINPROGRESS, "Operation now in
// progress").
func bootoutInProgress(out []byte, err error) bool {
	return exitCode(err) == 36 || strings.Contains(string(out), "Operation now in progress")
}

func (m *launchdManager) hasGUISession() bool {
	_, err := m.run.Run("launchctl", "print", m.domain())
	return err == nil
}

// preflight checks that the agent can be loaded at all: a LaunchAgent only
// runs inside a GUI login session, which an SSH-only login does not create.
func (m *launchdManager) preflight() error {
	if err := m.checkUser(); err != nil {
		return err
	}
	if !m.hasGUISession() {
		return m.noGUISessionError()
	}
	return nil
}

func (m *launchdManager) noGUISessionError() error {
	return fmt.Errorf("launchd domain %s does not exist: a LaunchAgent needs a logged-in GUI session "+
		"for this user; log in at the console (or via Screen Sharing) and re-run this command, "+
		"or enable auto-login on a headless Mac", m.domain())
}

// checkInstalled fails when the plist is missing, since there is nothing to
// bootstrap.
func (m *launchdManager) checkInstalled() error {
	if _, err := os.Stat(m.plistPath()); err != nil {
		return fmt.Errorf("LaunchAgent is not installed (%v): run `immich-backup daemon install`", err)
	}
	return nil
}

func (m *launchdManager) Install(cfg *config.Config) error {
	if err := m.preflight(); err != nil {
		return err
	}
	bin, err := StableExecutable()
	if err != nil {
		return err
	}
	env, err := ResolveServiceEnv()
	if err != nil {
		return err
	}
	plist, err := GeneratePlist(bin, cfg, env)
	if err != nil {
		return fmt.Errorf("generate plist: %w", err)
	}
	if err := EnsureLogFile(cfg.Daemon.LogPath); err != nil {
		return fmt.Errorf("prepare daemon log (check daemon.log_path): %w", err)
	}
	path := m.plistPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(plist), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}
	return m.activate()
}

// activate (re)loads the plist into the GUI domain. launchd keeps the job
// definition it loaded until a bootout, so the old job is booted out first
// for a changed schedule or binary path to take effect. enable clears a
// disabled override (left by `unload -w` or `launchctl disable`) that would
// make bootstrap fail, and the final print proves the job is really loaded.
func (m *launchdManager) activate() error {
	if err := m.bootout(); err != nil {
		return err
	}
	return m.load()
}

// load enables and bootstraps the job, then checks it is loaded.
func (m *launchdManager) load() error {
	if _, err := m.launchctl("enable", m.target()); err != nil {
		return err
	}
	if _, err := m.launchctl("bootstrap", m.domain(), m.plistPath()); err != nil {
		return err
	}
	if out, err := m.run.Run("launchctl", "print", m.target()); err != nil {
		return fmt.Errorf("%s is not loaded after launchctl bootstrap: %w",
			m.target(), cmdError("launchctl", []string{"print", m.target()}, out, err))
	}
	return nil
}

// bootout unloads the job, stopping a running backup (launchd sends it
// SIGTERM). A job that is not loaded is not an error. For a running job
// launchctl can return "Operation now in progress" before launchd has
// finished stopping it, so bootout waits until the job is gone; a bootstrap
// before that fails with "Input/output error" because the old instance is
// still registered.
func (m *launchdManager) bootout() error {
	args := []string{"bootout", m.target()}
	out, err := m.run.Run("launchctl", args...)
	switch {
	case err == nil || notLoaded(out, err):
		return nil
	case bootoutInProgress(out, err):
		return m.waitUnloaded()
	default:
		return cmdError("launchctl", args, out, err)
	}
}

// waitUnloaded polls launchctl print until the job is no longer loaded,
// failing after bootoutTimeout.
func (m *launchdManager) waitUnloaded() error {
	args := []string{"print", m.target()}
	for waited := time.Duration(0); ; waited += bootoutPoll {
		out, err := m.run.Run("launchctl", args...)
		if err != nil {
			if notLoaded(out, err) {
				return nil
			}
			return cmdError("launchctl", args, out, err)
		}
		if waited >= bootoutTimeout {
			return fmt.Errorf("%s is still loaded %s after launchctl bootout: the running backup "+
				"has not exited yet; wait for it to finish and re-run this command", m.target(), bootoutTimeout)
		}
		m.sleep(bootoutPoll)
	}
}

func (m *launchdManager) Uninstall() error {
	if err := m.checkUser(); err != nil {
		return err
	}
	// Without a GUI session the domain does not exist, so nothing is loaded.
	if m.hasGUISession() {
		if err := m.bootout(); err != nil {
			return err
		}
	}
	if err := os.Remove(m.plistPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}
	return nil
}

// Start loads the job so the schedule runs again. It never runs a backup
// immediately; a loaded job is left as it is.
func (m *launchdManager) Start() error {
	if err := m.checkInstalled(); err != nil {
		return err
	}
	if err := m.preflight(); err != nil {
		return err
	}
	if _, err := m.run.Run("launchctl", "print", m.target()); err == nil {
		return nil
	}
	return m.load()
}

// Stop unloads the job so no further scheduled backups run until the next
// start, restart, install or GUI login (launchd reloads ~/Library/LaunchAgents
// at login). The plist is kept.
func (m *launchdManager) Stop() error {
	if err := m.checkUser(); err != nil {
		return err
	}
	if !m.hasGUISession() {
		return nil
	}
	return m.bootout()
}

func (m *launchdManager) Restart() error {
	if err := m.checkInstalled(); err != nil {
		return err
	}
	if err := m.preflight(); err != nil {
		return err
	}
	return m.activate()
}
