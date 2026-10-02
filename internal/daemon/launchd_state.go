// internal/daemon/launchd_state.go
package daemon

import (
	"encoding/xml"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// neverExited is parseLaunchctlPrint's last exit code for a job that has not
// exited since it was loaded.
const neverExited = -1

// parseLaunchctlPrint reads the job's state ("waiting", "running", ...), its
// last exit code (neverExited when none) and the number of runs since it was
// loaded from `launchctl print <target>`. Only top-level properties are read;
// nested dicts (environment, event triggers) can repeat the same keys.
func parseLaunchctlPrint(out []byte) (state string, lastExit int, runs int) {
	lastExit = neverExited
	depth := 0
	for _, raw := range strings.Split(string(out), "\n") {
		line := strings.TrimSpace(raw)
		if line == "}" {
			depth--
			continue
		}
		opens := strings.HasSuffix(line, "{")
		if depth <= 1 {
			if k, v, ok := strings.Cut(line, " = "); ok {
				switch k {
				case "state":
					state = v
				case "runs":
					runs, _ = strconv.Atoi(v)
				case "last exit code", "last exit status":
					// e.g. "0", "78: EX_CONFIG" or "(never exited)".
					num, _, _ := strings.Cut(v, ":")
					if n, err := strconv.Atoi(strings.TrimSpace(num)); err == nil {
						lastExit = n
					}
				}
			}
		}
		if opens {
			depth++
		}
	}
	return state, lastExit, runs
}

// launchdExitHints explains exit codes of a scheduled run.
var launchdExitHints = map[int]string{
	1: "the backup ran and failed; see `immich-backup daemon logs` and `immich-backup status`",
	78: "EX_CONFIG: launchd could not set up the job, often because the log directory or the program " +
		"no longer exists; re-run `immich-backup daemon install`",
}

// plistInfo is what launchdState and Definition need from an installed plist.
type plistInfo struct {
	Program      string
	LogPath      string
	Hour, Minute int // -1 when StartCalendarInterval does not set them
}

// plistNode is a generic plist XML element.
type plistNode struct {
	XMLName xml.Name
	Content string      `xml:",chardata"`
	Nodes   []plistNode `xml:",any"`
}

// pairs returns a dict node's key/value element pairs.
func (n plistNode) pairs() map[string]plistNode {
	m := map[string]plistNode{}
	for i := 0; i+1 < len(n.Nodes); i += 2 {
		if n.Nodes[i].XMLName.Local == "key" {
			m[strings.TrimSpace(n.Nodes[i].Content)] = n.Nodes[i+1]
		}
	}
	return m
}

// parsePlist reads the program, log path and daily schedule back from a
// plist written by GeneratePlist.
func parsePlist(data []byte) (plistInfo, error) {
	info := plistInfo{Hour: -1, Minute: -1}
	var root plistNode
	if err := xml.Unmarshal(data, &root); err != nil {
		return info, err
	}
	var dict plistNode
	for _, n := range root.Nodes {
		if n.XMLName.Local == "dict" {
			dict = n
			break
		}
	}
	keys := dict.pairs()
	if args, ok := keys["ProgramArguments"]; ok && len(args.Nodes) > 0 {
		info.Program = args.Nodes[0].Content
	}
	if p, ok := keys["Program"]; ok {
		info.Program = p.Content
	}
	if p, ok := keys["StandardOutPath"]; ok {
		info.LogPath = p.Content
	}
	if cal, ok := keys["StartCalendarInterval"]; ok {
		times := cal.pairs()
		if h, err := strconv.Atoi(strings.TrimSpace(times["Hour"].Content)); err == nil {
			info.Hour = h
		}
		if m, err := strconv.Atoi(strings.TrimSpace(times["Minute"].Content)); err == nil {
			info.Minute = m
		}
	}
	return info, nil
}

// launchdState builds the service state. loaded reports whether `launchctl
// print` found the job (out is its output), gui whether the user's GUI
// domain exists, and noGUI explains a missing one.
func launchdState(installed, loaded, gui bool, out []byte, info plistInfo, now time.Time, noGUI string) State {
	st := State{Installed: installed, Runs: -1}
	switch {
	case !installed:
		st.Status = "not installed"
		st.Problems = append(st.Problems, notInstalledProblem)
		return st
	case !loaded:
		st.Status = "not loaded"
		if !gui {
			st.Problems = append(st.Problems, noGUI)
		} else {
			st.Problems = append(st.Problems, "the LaunchAgent is not loaded, so no backups are scheduled: "+
				"run `immich-backup daemon start` (or `immich-backup daemon install` to rewrite it)")
		}
		return st
	}
	st.Active = true
	state, lastExit, runs := parseLaunchctlPrint(out)
	st.Status = "loaded"
	if state != "" {
		st.Status += ", " + state
	}
	st.Runs = runs
	if info.Hour >= 0 && info.Minute >= 0 {
		st.NextRun = nextDailyRun(now, info.Hour, info.Minute).Format("Mon 2006-01-02 15:04 MST")
	}
	switch {
	case state == "running":
		st.LastResult = "running now"
	case lastExit == 0:
		st.LastResult = "succeeded"
	case lastExit > 0:
		st.LastFailed = true
		st.LastResult = fmt.Sprintf("failed (exit code %d)", lastExit)
		hint, ok := launchdExitHints[lastExit]
		if !ok {
			hint = "see `immich-backup daemon logs`"
		}
		st.Problems = append(st.Problems, fmt.Sprintf("the last scheduled backup failed (exit code %d): %s", lastExit, hint))
	}
	return st
}

// readPlist reads and parses the installed plist; ok is false when it does
// not exist.
func (m *launchdManager) readPlist() (info plistInfo, ok bool, err error) {
	data, err := os.ReadFile(m.plistPath())
	if os.IsNotExist(err) {
		return plistInfo{Hour: -1, Minute: -1}, false, nil
	}
	if err != nil {
		return plistInfo{}, false, fmt.Errorf("read plist: %w", err)
	}
	info, err = parsePlist(data)
	if err != nil {
		return plistInfo{}, true, fmt.Errorf("parse %s: %w", m.plistPath(), err)
	}
	return info, true, nil
}

// State queries launchd for the job. launchd records no run times, so the
// next run is computed from the installed plist's schedule and the last run
// is described by its exit code and run count.
func (m *launchdManager) State() (State, error) {
	if err := m.checkUser(); err != nil {
		return State{}, err
	}
	info, installed, err := m.readPlist()
	if err != nil {
		return State{}, err
	}
	args := []string{"print", m.target()}
	out, err := m.run.Run("launchctl", args...)
	loaded := err == nil
	if err != nil && !notLoaded(out, err) {
		return State{}, cmdError("launchctl", args, out, err)
	}
	gui := loaded || m.hasGUISession()
	return launchdState(installed, loaded, gui, out, info, m.now(), m.noGUISessionError().Error()), nil
}

// Definition reads the installed plist; nil when it is not installed.
func (m *launchdManager) Definition() (*Definition, error) {
	info, installed, err := m.readPlist()
	if err != nil || !installed {
		return nil, err
	}
	return &Definition{Path: m.plistPath(), BinaryPath: info.Program, LogPath: info.LogPath}, nil
}

// Status reports whether the job is loaded, the next run and the last exit
// code. Problems that stop scheduled backups are returned as the error,
// along with the text.
func (m *launchdManager) Status() (string, error) {
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

// Logs shows launchd's view of the job (whether it is loaded, its runs and
// last exit code) followed by the tail of the plist's StandardOutPath.
func (m *launchdManager) Logs() (string, error) {
	def, err := m.Definition()
	if err != nil {
		return "", err
	}
	if def == nil {
		return "", fmt.Errorf("%s (the backup log is still shown by `immich-backup logs`)", notInstalledProblem)
	}
	var b strings.Builder
	b.WriteString("── launchd (" + m.target() + ") ──\n")
	if text, err := m.Status(); text != "" || err != nil {
		b.WriteString(text)
		if err != nil {
			b.WriteString("Problem:   " + strings.ReplaceAll(err.Error(), "\n", "\nProblem:   ") + "\n")
		}
	}
	b.WriteString("\n" + logSection(def.LogPath))
	return b.String(), nil
}
