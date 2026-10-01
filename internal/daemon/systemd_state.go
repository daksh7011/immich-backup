// internal/daemon/systemd_state.go
package daemon

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

var timerProps = []string{"LoadState", "ActiveState", "SubState", "UnitFileState", "NextElapseUSecRealtime", "LastTriggerUSec"}
var serviceProps = []string{"ActiveState", "Result", "ExecMainStatus", "ExecMainStartTimestamp", "ExecMainExitTimestamp"}

// parseSystemctlShow parses `systemctl show -p ...` output, one Key=Value per
// line. Unlike `systemctl status`, show exits 0 for inactive and unknown
// units, so its output is the state rather than an error.
func parseSystemctlShow(out []byte) map[string]string {
	props := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(strings.TrimRight(line, "\r"), "="); ok {
			props[k] = v
		}
	}
	return props
}

// systemdTime normalises a timestamp property: "" for unset ("", "n/a", 0),
// microseconds since the epoch (older systemd) as local time, and systemd's
// own formatted timestamp as it is.
func systemdTime(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || v == "n/a" {
		return ""
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		if n <= 0 {
			return ""
		}
		return time.UnixMicro(n).Local().Format("Mon 2006-01-02 15:04:05 MST")
	}
	return v
}

// systemdExitHints explains the exit statuses systemd itself uses when it
// cannot start the backup, and the backup's own failure status.
var systemdExitHints = map[string]string{
	"1": "the backup ran and failed; see `immich-backup daemon logs` and `immich-backup status`",
	"203": "203/EXEC: the program in ExecStart is missing or not executable (moved or upgraded?); " +
		"re-run `immich-backup daemon install`",
	"209": "209/STDOUT: systemd could not open the log file (missing directory?); " +
		"re-run `immich-backup daemon install`",
}

// systemdState builds the service state from `systemctl show` properties of
// the timer and the service, plus the user's linger state. installed reports
// whether the timer file exists.
func systemdState(installed bool, timer, svc map[string]string, user, linger string) State {
	st := State{Installed: installed, Runs: -1, User: user, Linger: linger}
	if !installed {
		st.Status = "not installed"
		st.Problems = append(st.Problems, notInstalledProblem)
		return st
	}
	if timer["LoadState"] != "loaded" {
		st.Status = "not loaded (" + timer["LoadState"] + ")"
		st.Problems = append(st.Problems, "the unit files exist but systemd has not loaded them: "+
			"run `immich-backup daemon install`")
	} else {
		st.Status = timer["ActiveState"]
		if sub := timer["SubState"]; sub != "" {
			st.Status += " (" + sub + ")"
		}
		if uf := timer["UnitFileState"]; uf != "" {
			st.Status += ", " + uf
		}
		st.Active = timer["ActiveState"] == "active"
		switch {
		case !st.Active:
			st.Problems = append(st.Problems, fmt.Sprintf("the backup timer is %s, so no backups are scheduled: "+
				"run `immich-backup daemon start`", timer["ActiveState"]))
		case timer["UnitFileState"] != "enabled":
			st.Problems = append(st.Problems, "the backup timer is not enabled, so it does not start again "+
				"after a reboot or re-login: run `immich-backup daemon install`")
		}
		if st.Active {
			st.NextRun = systemdTime(timer["NextElapseUSecRealtime"])
		}
	}

	// ExecMainStartTimestamp and ExecMainStatus describe the last run this
	// user manager started. After a reboot or a manager restart they are
	// empty, and only the timer's LastTriggerUSec (kept on disk because of
	// Persistent=true) says when the backup last ran, not how it went.
	started := systemdTime(svc["ExecMainStartTimestamp"])
	st.LastRun = started
	if st.LastRun == "" {
		st.LastRun = systemdTime(timer["LastTriggerUSec"])
	}
	code := svc["ExecMainStatus"]
	switch result := svc["Result"]; {
	case svc["ActiveState"] == "activating":
		st.LastResult = "running now"
	case result != "" && result != "success":
		st.LastFailed = true
		desc := result
		if code != "" && code != "0" {
			desc = fmt.Sprintf("%s, status %s", result, code)
		}
		st.LastResult = "failed (" + desc + ")"
		hint, ok := systemdExitHints[code]
		if !ok {
			hint = "see `immich-backup daemon logs`"
		}
		st.Problems = append(st.Problems, fmt.Sprintf("the last scheduled backup failed (%s): %s", desc, hint))
	case started != "" && (code == "" || code == "0"):
		st.LastResult = "succeeded"
	case started != "":
		// Result=success with a non-zero status: reset-failed (run by
		// `daemon install` and `daemon restart`) cleared the failure but not
		// the exit status of the run that failed.
		st.LastResult = fmt.Sprintf("failed with exit status %s (cleared by a reinstall or restart)", code)
	case st.LastRun != "":
		st.LastResult = "result not known since systemd restarted; see `immich-backup status`"
	}

	if linger == "no" {
		st.Problems = append(st.Problems, lingerProblem(user))
	}
	return st
}

// lingerProblem explains why linger matters and gives the command that
// enables it.
func lingerProblem(user string) string {
	return fmt.Sprintf("lingering is off for user %q, so systemd stops the backup timer when you log out "+
		"and scheduled backups never run on a headless server: run `sudo loginctl enable-linger %s`", user, user)
}

// parseSystemdUnit reads the program and log path back from a unit written
// by GenerateSystemdUnit. Older installs wrote ExecStart unquoted, so an
// unquoted first word is accepted too.
func parseSystemdUnit(data []byte) Definition {
	var def Definition
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "ExecStart="):
			v := strings.TrimPrefix(line, "ExecStart=")
			if strings.HasPrefix(v, `"`) {
				def.BinaryPath = systemdUnquote(v)
			} else if f := strings.Fields(v); len(f) > 0 {
				def.BinaryPath = strings.ReplaceAll(f[0], "%%", "%")
			}
		case strings.HasPrefix(line, "StandardOutput="):
			v := strings.TrimPrefix(line, "StandardOutput=")
			for _, p := range []string{"append:", "file:", "truncate:"} {
				if strings.HasPrefix(v, p) {
					def.LogPath = strings.ReplaceAll(strings.TrimPrefix(v, p), "%%", "%")
				}
			}
		}
	}
	return def
}

// systemdUnquote returns the first double-quoted word of s, undoing
// systemdQuote: backslash escapes (\\, \", \n, \t, \xNN) and %%.
func systemdUnquote(s string) string {
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			return b.String()
		case c == '\\' && i+1 < len(s):
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'x':
				if i+2 < len(s) {
					if n, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
						b.WriteByte(byte(n))
						i += 2
						continue
					}
				}
				b.WriteByte('x')
			default:
				b.WriteByte(s[i])
			}
		case c == '%' && i+1 < len(s) && s[i+1] == '%':
			b.WriteByte('%')
			i++
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// show runs `systemctl --user show` for unit with the given properties.
func (m *systemdManager) show(unit string, props []string) (map[string]string, error) {
	args := []string{"--user", "show", unit}
	for _, p := range props {
		args = append(args, "-p", p)
	}
	out, err := m.command("systemctl", args...)
	if err != nil {
		return nil, err
	}
	return parseSystemctlShow(out), nil
}

// lingerUser is the name loginctl knows this user by.
func (m *systemdManager) lingerUser() string {
	name, err := m.username()
	if err != nil {
		// loginctl accepts a numeric UID wherever it takes a user name.
		return strconv.Itoa(m.geteuid())
	}
	return name
}

// lingerState returns "yes", "no" or "unknown (...)" for the user's linger
// setting. show-user fails with "not logged in or lingering" exactly when
// linger is off and the user has no session, so that failure means "no".
func (m *systemdManager) lingerState(name string) string {
	args := []string{"show-user", name, "-p", "Linger", "--value"}
	out, err := m.run.Run("loginctl", args...)
	if err != nil {
		if strings.Contains(string(out), "not logged in or lingering") {
			return "no"
		}
		return "unknown (" + cmdError("loginctl", args, out, err).Error() + ")"
	}
	if parseLinger(out) {
		return "yes"
	}
	return "no"
}

// State queries systemd for the timer, the last run of the service and the
// linger setting.
func (m *systemdManager) State() (State, error) {
	if err := m.preflight(); err != nil {
		return State{}, err
	}
	timer, err := m.show(timerName, timerProps)
	if err != nil {
		return State{}, err
	}
	svc, err := m.show(unitName, serviceProps)
	if err != nil {
		return State{}, err
	}
	name := m.lingerUser()
	return systemdState(fileExists(m.timerPath()), timer, svc, name, m.lingerState(name)), nil
}

// Definition reads the installed service unit; nil when it is not installed.
func (m *systemdManager) Definition() (*Definition, error) {
	path := m.unitPath()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read unit file: %w", err)
	}
	def := parseSystemdUnit(data)
	def.Path = path
	return &def, nil
}

// Status reports the timer, the next and last run, and linger. Problems
// that stop scheduled backups are returned as the error, along with the text.
func (m *systemdManager) Status() (string, error) {
	st, err := m.State()
	if err != nil {
		return "", err
	}
	def, err := m.Definition()
	if err != nil {
		return "", err
	}
	return report(st, def)
}

// Logs shows systemd's own messages about the timer and service (start
// failures such as 203/EXEC and 209/STDOUT land only there) followed by the
// tail of the log file the scheduled run writes to.
func (m *systemdManager) Logs() (string, error) {
	def, err := m.Definition()
	if err != nil {
		return "", err
	}
	if def == nil {
		return "", fmt.Errorf("%s (the backup log is still shown by `immich-backup logs`)", notInstalledProblem)
	}
	var b strings.Builder
	b.WriteString("── systemd journal (" + unitName + ", " + timerName + ") ──\n")
	// --user-unit without --user: --user reads only the per-user journal
	// files, which journald writes only on persistent storage. --user-unit
	// matches the units and the user manager's messages about them in every
	// journal this user may read, the same way across systemd versions.
	// --quiet drops the hint about other users' messages that journalctl
	// prints for users outside the systemd-journal and adm groups.
	args := []string{"--user-unit=" + unitName, "--user-unit=" + timerName, "-n", "50", "--no-pager", "--quiet"}
	out, err := m.run.Run("journalctl", args...)
	switch {
	case err != nil:
		// An unreadable or empty journal is common; the log file below still
		// has the backup's own output.
		b.WriteString("(" + cmdError("journalctl", args, out, err).Error() + ")\n")
	case len(bytes.TrimSpace(out)) == 0:
		// --quiet also drops journalctl's own "-- No entries --".
		b.WriteString("(no entries)\n")
	default:
		b.WriteString(strings.TrimRight(string(out), "\n") + "\n")
	}
	b.WriteString("\n" + logSection(def.LogPath))
	return b.String(), nil
}
