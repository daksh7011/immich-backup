// internal/daemon/systemd.go
package daemon

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/daksh7011/immich-backup/internal/config"
)

const unitName = "immich-backup.service"
const timerName = "immich-backup.timer"

// The service has no After=network.target: that target does not exist in a
// user manager, so the ordering was a no-op.
var unitTmpl = template.Must(template.New("unit").Parse(`[Unit]
Description=immich-backup media and database backup service

[Service]
Type=oneshot
{{- range .Env}}
Environment={{.}}
{{- end}}
ExecStart={{.ExecPath}} backup
StandardOutput=append:{{.LogPath}}
StandardError=append:{{.LogPath}}
`))

// The timer activates immich-backup.service by its matching name. It must not
// Require= the service: that starts a backup every time the timer starts
// (install, start, each login or boot) and stopping the service stops the
// timer.
var timerTmpl = template.Must(template.New("timer").Parse(`[Unit]
Description=immich-backup scheduled backup

[Timer]
OnCalendar={{.OnCalendar}}
Persistent=true

[Install]
WantedBy=timers.target
`))

// GenerateSystemdUnit returns the systemd service unit file content. The
// binary path is quoted so spaces survive ExecStart word splitting, and % is
// escaped as %% there and in the log path so systemd does not expand it as a
// specifier. Each env variable becomes a quoted Environment= line; systemd
// does not expand $VARS there, so values are written literally.
// Exported for testing.
func GenerateSystemdUnit(binaryPath string, cfg *config.Config, env ServiceEnv) string {
	var envLines []string
	for _, v := range env.vars() {
		envLines = append(envLines, systemdQuote(v.Key+"="+v.Value))
	}
	var buf strings.Builder
	_ = unitTmpl.Execute(&buf, map[string]any{
		"ExecPath": systemdQuote(binaryPath),
		"LogPath":  strings.ReplaceAll(cfg.Daemon.LogPath, "%", "%%"),
		"Env":      envLines,
	})
	return buf.String()
}

// GenerateSystemdTimer returns the systemd timer unit file content derived
// from the cron schedule in cfg. Returns an error if the schedule uses step
// expressions (e.g. */6) which cannot be directly expressed as a single
// OnCalendar entry.
// Exported for testing.
func GenerateSystemdTimer(schedule string) (string, error) {
	onCal, err := cronToOnCalendar(schedule)
	if err != nil {
		return "", err
	}
	var buf strings.Builder
	if err := timerTmpl.Execute(&buf, map[string]string{"OnCalendar": onCal}); err != nil {
		return "", fmt.Errorf("render timer template: %w", err)
	}
	return buf.String(), nil
}

// cronToOnCalendar converts a simple "MINUTE HOUR * * *" cron expression to a
// systemd OnCalendar value (e.g. "*-*-* 03:00:00"). Returns an error for
// step, range, or list expressions in the minute or hour fields.
func cronToOnCalendar(schedule string) (string, error) {
	parts := strings.Fields(schedule)
	if len(parts) != 5 {
		return "", fmt.Errorf("schedule must have exactly 5 cron fields, got %d", len(parts))
	}
	minute, hour := parts[0], parts[1]
	if !isSimpleInt(minute) || !isSimpleInt(hour) {
		return "", fmt.Errorf(
			"daemon scheduling only supports simple hour/minute values (e.g. \"0 3 * * *\"); "+
				"step/range/list expressions like %q are not supported — use a specific time",
			schedule,
		)
	}
	m, _ := strconv.Atoi(minute)
	h, _ := strconv.Atoi(hour)
	return fmt.Sprintf("*-*-* %02d:%02d:00", h, m), nil
}

func unitPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user", unitName)
}

func timerPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user", timerName)
}

type systemdManager struct {
	run       runner
	geteuid   func() int
	getenv    func(string) string
	username  func() (string, error)
	unitPath  func() string
	timerPath func() string
}

func newSystemdManager() *systemdManager {
	return &systemdManager{
		run:       execRunner{},
		geteuid:   os.Geteuid,
		getenv:    os.Getenv,
		username:  currentUsername,
		unitPath:  unitPath,
		timerPath: timerPath,
	}
}

// currentUsername returns the current user's login name. Without CGo, os/user
// only reads /etc/passwd, so fall back to $USER and $LOGNAME for directory
// accounts (LDAP, SSSD).
func currentUsername() (string, error) {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username, nil
	}
	for _, k := range []string{"USER", "LOGNAME"} {
		if v := os.Getenv(k); v != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("cannot determine the current user name")
}

// command runs name with args and, on failure, returns an error carrying the
// command line and its output, which names the real problem (no user bus,
// unit not found, access denied).
func (m *systemdManager) command(name string, args ...string) ([]byte, error) {
	out, err := m.run.Run(name, args...)
	if err != nil {
		return out, cmdError(name, args, out, err)
	}
	return out, nil
}

func (m *systemdManager) systemctl(args ...string) error {
	_, err := m.command("systemctl", append([]string{"--user"}, args...)...)
	return err
}

// preflight checks that systemctl --user will reach this user's own manager.
// As root it would install into root's manager instead, and without
// XDG_RUNTIME_DIR (sudo or su without a login session) systemctl cannot find
// the user bus.
func (m *systemdManager) preflight() error {
	uid := m.geteuid()
	if uid == 0 {
		return fmt.Errorf("refusing to manage the systemd user service as root: " +
			"run immich-backup without sudo, as the user that should own the backups, " +
			"from that user's own login session (e.g. ssh <user>@<host>)")
	}
	if m.getenv("XDG_RUNTIME_DIR") == "" {
		return fmt.Errorf("XDG_RUNTIME_DIR is not set, so systemctl --user cannot reach your user manager: "+
			"log in as this user directly (e.g. over SSH) instead of via sudo or su, "+
			"or run `export XDG_RUNTIME_DIR=/run/user/%d` and retry", uid)
	}
	return nil
}

func (m *systemdManager) Install(cfg *config.Config) error {
	if err := m.preflight(); err != nil {
		return err
	}
	bin, err := StableExecutable()
	if err != nil {
		return err
	}
	if err := checkUnitPath(cfg.Daemon.LogPath); err != nil {
		return fmt.Errorf("check daemon.log_path: %w", err)
	}
	env, err := ResolveServiceEnv()
	if err != nil {
		return err
	}
	unit := GenerateSystemdUnit(bin, cfg, env)
	timerContent, err := GenerateSystemdTimer(cfg.Backup.Schedule)
	if err != nil {
		return fmt.Errorf("generate timer: %w", err)
	}
	if err := EnsureLogFile(cfg.Daemon.LogPath); err != nil {
		return fmt.Errorf("prepare daemon log (check daemon.log_path): %w", err)
	}
	uPath := m.unitPath()
	tPath := m.timerPath()
	if err := os.MkdirAll(filepath.Dir(uPath), 0755); err != nil {
		return fmt.Errorf("create systemd user dir: %w", err)
	}
	if err := os.WriteFile(uPath, []byte(unit), 0644); err != nil {
		return fmt.Errorf("write unit file: %w", err)
	}
	if err := os.WriteFile(tPath, []byte(timerContent), 0644); err != nil {
		return fmt.Errorf("write timer file: %w", err)
	}
	return m.activate()
}

// activate loads the written units, enables the timer and restarts it so an
// already-active timer picks up a changed OnCalendar, then makes sure the
// user manager keeps running after logout.
func (m *systemdManager) activate() error {
	if err := m.systemctl("daemon-reload"); err != nil {
		return err
	}
	m.resetFailed()
	if err := m.systemctl("enable", timerName); err != nil {
		return err
	}
	if err := m.systemctl("restart", timerName); err != nil {
		return err
	}
	return m.ensureLinger()
}

// ensureLinger makes the user manager, and so the timer, run without a login
// session. Without linger, logind stops user@UID.service when the last
// session ends and does not start it at boot, so on a headless server the
// timer never fires once you log out. polkit often refuses enable-linger
// without root over SSH, so a failure ends in the exact sudo command to run.
func (m *systemdManager) ensureLinger() error {
	name := m.lingerUser()
	// show-user also fails with "not logged in or lingering" when linger is
	// off, so a failed first check still goes on to enable it.
	if on, _ := m.lingerEnabled(name); on {
		return nil
	}
	_, enableErr := m.command("loginctl", "enable-linger", name)
	on, checkErr := m.lingerEnabled(name)
	if on {
		return nil
	}
	cause := enableErr
	if cause == nil {
		cause = checkErr
	}
	if cause == nil {
		cause = fmt.Errorf("Linger is still \"no\" after `loginctl enable-linger %s`", name)
	}
	return fmt.Errorf(
		"lingering is off for user %q, so systemd stops the backup timer when you log out "+
			"and scheduled backups never run on a headless server (%v). "+
			"The timer is installed; to keep it running, run:\n  sudo loginctl enable-linger %s",
		name, cause, name)
}

// resetFailed clears the failed state a oneshot service keeps until its next
// run, so `daemon status` stops reporting a failure that a reinstall or
// restart just fixed (a 203/EXEC or 209/STDOUT). The journal and the log file
// keep the history. It fails when the service never ran, which is fine.
func (m *systemdManager) resetFailed() {
	_ = m.systemctl("reset-failed", unitName)
}

func (m *systemdManager) lingerEnabled(name string) (bool, error) {
	out, err := m.command("loginctl", "show-user", name, "-p", "Linger", "--value")
	if err != nil {
		return false, err
	}
	return parseLinger(out), nil
}

// parseLinger reports whether loginctl's Linger property is "yes". It accepts
// both the --value form ("yes") and the key=value form ("Linger=yes").
func parseLinger(out []byte) bool {
	v := strings.TrimSpace(string(out))
	return strings.TrimPrefix(v, "Linger=") == "yes"
}

func (m *systemdManager) Uninstall() error {
	if err := m.preflight(); err != nil {
		return err
	}
	// stop and disable fail when the timer was never installed; that is fine
	// here, the goal is only that it is gone.
	_ = m.systemctl("stop", timerName)
	_ = m.systemctl("disable", timerName)
	_ = os.Remove(m.timerPath())
	_ = os.Remove(m.unitPath())
	return m.systemctl("daemon-reload")
}

func (m *systemdManager) Start() error {
	if err := m.preflight(); err != nil {
		return err
	}
	return m.systemctl("start", timerName)
}

func (m *systemdManager) Stop() error {
	if err := m.preflight(); err != nil {
		return err
	}
	return m.systemctl("stop", timerName)
}

func (m *systemdManager) Restart() error {
	if err := m.preflight(); err != nil {
		return err
	}
	m.resetFailed()
	return m.systemctl("restart", timerName)
}
