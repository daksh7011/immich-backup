// internal/daemon/state.go
package daemon

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// State is a snapshot of the scheduled service as the scheduler reports it,
// for `daemon status`, `status` and doctor.
type State struct {
	Installed  bool   // the unit or plist file exists
	Active     bool   // the timer is active / the job is loaded, so runs are scheduled
	Status     string // the scheduler's own state, e.g. "active (waiting), enabled"
	NextRun    string // next scheduled run; "" when unknown or not scheduled
	LastRun    string // when the last scheduled run started; "" when unknown
	LastResult string // "succeeded", "running now", "failed (...)" or why it is unknown; "" when no run is known
	LastFailed bool
	Runs       int    // runs since the job was loaded (launchd only); -1 when unknown
	Linger     string // systemd only: "yes", "no" or "unknown (...)"; "" elsewhere
	User       string // systemd only: the user linger applies to
	Problems   []string
}

// Definition is the installed service as the scheduler will run it, read back
// from the unit or plist file rather than the current config, which may have
// changed since `daemon install`.
type Definition struct {
	Path       string // unit or plist file
	BinaryPath string // program the scheduler executes; "" when it cannot be parsed
	LogPath    string // file the scheduled run's output goes to; "" when not set
}

// ErrUnsupported is returned by Detect on platforms without a supported
// service manager.
var ErrUnsupported = fmt.Errorf("the background service is not supported on %s (only Linux systemd and macOS launchd)", runtime.GOOS)

// notInstalledProblem is the problem reported when no unit or plist exists.
const notInstalledProblem = "the background service is not installed, so no backups are scheduled: " +
	"run `immich-backup daemon install`"

// report renders st and the installed definition (nil when not installed) as
// the `daemon status` text. Every problem is also returned as the error, so
// the command exits non-zero when scheduled backups are not going to run or
// the last one failed.
func report(st State, def *Definition) (string, error) {
	var b strings.Builder
	line := func(label, value string) {
		if value != "" {
			fmt.Fprintf(&b, "%-10s %s\n", label+":", value)
		}
	}
	line("Service", st.Status)
	next := st.NextRun
	if next == "" && st.Active {
		next = "unknown"
	}
	line("Next run", next)
	last := st.LastResult
	if st.LastRun != "" {
		last = strings.TrimSuffix(st.LastRun+", "+st.LastResult, ", ")
	}
	if last == "" && st.Installed {
		last = "none recorded since the service was loaded"
	}
	line("Last run", last)
	if st.Runs >= 0 {
		line("Runs", strconv.Itoa(st.Runs)+" since the job was loaded")
	}
	line("Linger", st.Linger)
	problems := st.Problems
	if def != nil {
		line("Program", def.BinaryPath)
		line("Log", def.LogPath)
		line("Defined", def.Path)
		if def.BinaryPath != "" {
			if err := CheckProgram(def.BinaryPath); err != nil {
				problems = append(problems, fmt.Sprintf("the installed service runs %s: %v; "+
					"re-run `immich-backup daemon install` (needed after moving or upgrading immich-backup)",
					def.BinaryPath, err))
			}
		}
	}
	if len(problems) > 0 {
		return b.String(), errors.New(strings.Join(problems, "\n"))
	}
	return b.String(), nil
}

// CheckProgram reports whether path is an existing executable file, as the
// scheduler needs it to be to start a backup.
func CheckProgram(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("the program does not exist (removed by an upgrade?)")
		}
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("the program is not a regular file")
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("the program is not executable")
	}
	return nil
}

// TailFile reads the last maxBytes bytes of the named file. If the file is
// larger than maxBytes, the result is trimmed to start on a line boundary so
// no partial log lines are returned.
func TailFile(name string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	start := size - maxBytes
	if start < 0 {
		start = 0
	}
	if _, err = f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	buf, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	// Drop the first (possibly partial) line when we seeked into the middle.
	if start > 0 {
		if i := bytes.IndexByte(buf, '\n'); i >= 0 {
			buf = buf[i+1:]
		}
	}
	return buf, nil
}

// logTailBytes bounds the log tail shown by `daemon logs`.
const logTailBytes = 64 << 10

// logSection renders the tail of the scheduled run's log file for `daemon
// logs`. A missing file is explained rather than reported as an error: the
// scheduler creates it on the first run, and cannot when the directory is gone.
func logSection(path string) string {
	if path == "" {
		return "── backup log ──\n(no log file is set in the installed service)\n"
	}
	head := "── " + path + " ──\n"
	data, err := TailFile(path, logTailBytes)
	switch {
	case os.IsNotExist(err):
		return head + "(no log file: no scheduled run has written output yet, or the scheduler " +
			"could not open it; see `immich-backup daemon status`)\n"
	case err != nil:
		return head + fmt.Sprintf("(cannot read the log: %v)\n", err)
	case len(bytes.TrimSpace(data)) == 0:
		return head + "(empty)\n"
	}
	return head + strings.TrimRight(string(data), "\n") + "\n"
}

// fileExists reports whether path exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// nextDailyRun returns the first time at hour:minute (in now's location)
// after now, which is when a daily launchd StartCalendarInterval fires.
func nextDailyRun(now time.Time, hour, minute int) time.Time {
	t := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	if !t.After(now) {
		t = t.AddDate(0, 0, 1)
	}
	return t
}
