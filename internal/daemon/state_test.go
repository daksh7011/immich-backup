package daemon_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/daemon"
)

// Captured `systemctl --user show` output for a healthy timer and for a
// service whose last run failed before the binary started (missing log dir).
const (
	timerShowActive = "LoadState=loaded\nActiveState=active\nSubState=waiting\nUnitFileState=enabled\n" +
		"NextElapseUSecRealtime=Sat 2026-10-03 03:00:00 CEST\nLastTriggerUSec=Fri 2026-10-02 03:00:00 CEST\n"
	serviceShowOK = "ActiveState=inactive\nResult=success\nExecMainStatus=0\n" +
		"ExecMainStartTimestamp=Fri 2026-10-02 03:00:00 CEST\nExecMainExitTimestamp=Fri 2026-10-02 03:04:10 CEST\n"
	serviceShow209 = "ActiveState=failed\nResult=exit-code\nExecMainStatus=209\n" +
		"ExecMainStartTimestamp=Fri 2026-10-02 03:00:00 CEST\nExecMainExitTimestamp=Fri 2026-10-02 03:00:00 CEST\n"
	serviceShowNeverRan = "ActiveState=inactive\nResult=success\nExecMainStatus=0\n" +
		"ExecMainStartTimestamp=\nExecMainExitTimestamp=\n"
)

const (
	timerShowCmd = "systemctl --user show immich-backup.timer -p LoadState -p ActiveState -p SubState " +
		"-p UnitFileState -p NextElapseUSecRealtime -p LastTriggerUSec"
	serviceShowCmd = "systemctl --user show immich-backup.service -p ActiveState -p Result -p ExecMainStatus " +
		"-p ExecMainStartTimestamp -p ExecMainExitTimestamp"
	journalCmd = "journalctl --user-unit=immich-backup.service --user-unit=immich-backup.timer -n 50 --no-pager --quiet"
)

// launchctlPrintFailed is captured `launchctl print` output for a loaded job
// whose last run exited 78. The nested event trigger repeats "state" and
// "runs", which must not be read.
const launchctlPrintFailed = `gui/501/com.immich-backup.agent = {
	active count = 0
	path = /Users/alice/Library/LaunchAgents/com.immich-backup.agent.plist
	type = LaunchAgent
	state = not running

	program = /opt/homebrew/bin/immich-backup
	arguments = {
		/opt/homebrew/bin/immich-backup
		backup
	}

	environment = {
		PATH => /opt/homebrew/bin:/usr/bin:/bin
	}

	runs = 3
	last exit code = 78: EX_CONFIG

	event triggers = {
		com.immich-backup.agent.268435461 => {
			keepalive = 0
			state = 1
			runs = 99
		}
	}
}
`

func TestParseSystemctlShow(t *testing.T) {
	props := daemon.ParseSystemctlShow([]byte(timerShowActive))
	if props["ActiveState"] != "active" || props["SubState"] != "waiting" {
		t.Errorf("props = %v", props)
	}
	if props["NextElapseUSecRealtime"] != "Sat 2026-10-03 03:00:00 CEST" {
		t.Errorf("NextElapseUSecRealtime = %q", props["NextElapseUSecRealtime"])
	}
	// Values may contain '='; only the first one splits.
	if got := daemon.ParseSystemctlShow([]byte("Environment=A=b\r\n"))["Environment"]; got != "A=b" {
		t.Errorf("Environment = %q, want A=b", got)
	}
}

func TestSystemdTime(t *testing.T) {
	for _, unset := range []string{"", "n/a", "0"} {
		if got := daemon.SystemdTime(unset); got != "" {
			t.Errorf("SystemdTime(%q) = %q, want empty", unset, got)
		}
	}
	if got := daemon.SystemdTime("Sat 2026-10-03 03:00:00 CEST"); got != "Sat 2026-10-03 03:00:00 CEST" {
		t.Errorf("formatted timestamp changed: %q", got)
	}
	// Older systemd prints microseconds since the epoch.
	usec := time.Date(2026, 10, 3, 3, 0, 0, 0, time.UTC).UnixMicro()
	if got := daemon.SystemdTime(strconv.FormatInt(usec, 10)); !strings.Contains(got, "2026-10-0") {
		t.Errorf("usec timestamp not converted: %q", got)
	}
}

func show(s string) map[string]string { return daemon.ParseSystemctlShow([]byte(s)) }

func TestSystemdState_Healthy(t *testing.T) {
	st := daemon.SystemdState(true, show(timerShowActive), show(serviceShowOK), "alice", "yes")
	if !st.Active || st.LastFailed || len(st.Problems) != 0 {
		t.Fatalf("healthy state reported problems: %+v", st)
	}
	if st.NextRun != "Sat 2026-10-03 03:00:00 CEST" {
		t.Errorf("NextRun = %q", st.NextRun)
	}
	if st.LastRun != "Fri 2026-10-02 03:00:00 CEST" || st.LastResult != "succeeded" {
		t.Errorf("last run = %q %q", st.LastRun, st.LastResult)
	}
	if st.Status != "active (waiting), enabled" {
		t.Errorf("Status = %q", st.Status)
	}
}

func TestSystemdState_FailedRunExplainsStatus(t *testing.T) {
	st := daemon.SystemdState(true, show(timerShowActive), show(serviceShow209), "alice", "yes")
	if !st.LastFailed || !strings.Contains(st.LastResult, "209") {
		t.Fatalf("expected a failed last run with status 209, got %+v", st)
	}
	p := strings.Join(st.Problems, "\n")
	if !strings.Contains(p, "209/STDOUT") || !strings.Contains(p, "daemon install") {
		t.Errorf("problem should explain 209 and give the remedy: %s", p)
	}
}

func TestSystemdState_NeverRan(t *testing.T) {
	timer := strings.Replace(timerShowActive, "LastTriggerUSec=Fri 2026-10-02 03:00:00 CEST", "LastTriggerUSec=n/a", 1)
	st := daemon.SystemdState(true, show(timer), show(serviceShowNeverRan), "alice", "yes")
	if st.LastRun != "" || st.LastResult != "" || len(st.Problems) != 0 {
		t.Errorf("a timer that never fired should report no run and no problem: %+v", st)
	}
}

func TestSystemdState_RunningNow(t *testing.T) {
	svc := strings.Replace(serviceShowOK, "ActiveState=inactive", "ActiveState=activating", 1)
	st := daemon.SystemdState(true, show(timerShowActive), show(svc), "alice", "yes")
	if st.LastResult != "running now" {
		t.Errorf("LastResult = %q, want running now", st.LastResult)
	}
}

// reset-failed (run by install and restart) sets Result back to success but
// keeps the failed run's exit status and start time; that run must not be
// reported as succeeded, nor as a current problem.
func TestSystemdState_ResetFailedRunIsNotSuccess(t *testing.T) {
	svc := strings.Replace(serviceShow209, "ActiveState=failed\nResult=exit-code", "ActiveState=inactive\nResult=success", 1)
	st := daemon.SystemdState(true, show(timerShowActive), show(svc), "alice", "yes")
	if st.LastRun != "Fri 2026-10-02 03:00:00 CEST" {
		t.Errorf("LastRun = %q", st.LastRun)
	}
	if strings.Contains(st.LastResult, "succeeded") || !strings.Contains(st.LastResult, "209") {
		t.Errorf("LastResult = %q, want the cleared 209 failure", st.LastResult)
	}
	if st.LastFailed || len(st.Problems) != 0 {
		t.Errorf("a cleared failure must not be a problem: %+v", st)
	}
}

// After a reboot or user-manager restart the service is fresh (Result=success,
// no start time) while the persistent timer still knows its last trigger: the
// run's outcome is unknown, not a success.
func TestSystemdState_AfterRestartResultUnknown(t *testing.T) {
	st := daemon.SystemdState(true, show(timerShowActive), show(serviceShowNeverRan), "alice", "yes")
	if st.LastRun != "Fri 2026-10-02 03:00:00 CEST" {
		t.Errorf("LastRun = %q, want the timer's last trigger", st.LastRun)
	}
	if strings.Contains(st.LastResult, "succeeded") || !strings.Contains(st.LastResult, "not known") {
		t.Errorf("LastResult = %q, want an unknown result", st.LastResult)
	}
	if st.LastFailed || len(st.Problems) != 0 {
		t.Errorf("an unknown result must not be a problem: %+v", st)
	}
}

func TestSystemdState_LingerOff(t *testing.T) {
	st := daemon.SystemdState(true, show(timerShowActive), show(serviceShowOK), "alice", "no")
	p := strings.Join(st.Problems, "\n")
	if !strings.Contains(p, "sudo loginctl enable-linger alice") {
		t.Errorf("linger off should give the sudo remedy, got %q", p)
	}
}

func TestSystemdState_TimerInactive(t *testing.T) {
	timer := "LoadState=loaded\nActiveState=inactive\nSubState=dead\nUnitFileState=enabled\nNextElapseUSecRealtime=\n"
	st := daemon.SystemdState(true, show(timer), show(serviceShowNeverRan), "alice", "yes")
	if st.Active || st.NextRun != "" {
		t.Errorf("inactive timer must not be active or have a next run: %+v", st)
	}
	if p := strings.Join(st.Problems, "\n"); !strings.Contains(p, "daemon start") {
		t.Errorf("inactive timer should suggest daemon start, got %q", p)
	}
}

func TestSystemdState_TimerDisabled(t *testing.T) {
	timer := strings.Replace(timerShowActive, "UnitFileState=enabled", "UnitFileState=disabled", 1)
	st := daemon.SystemdState(true, show(timer), show(serviceShowOK), "alice", "yes")
	if p := strings.Join(st.Problems, "\n"); !strings.Contains(p, "not enabled") {
		t.Errorf("disabled timer should be a problem, got %q", p)
	}
}

func TestSystemdState_NotInstalled(t *testing.T) {
	st := daemon.SystemdState(false, show("LoadState=not-found\nActiveState=inactive\n"), show(serviceShowNeverRan), "alice", "no")
	if st.Installed || st.Status != "not installed" {
		t.Errorf("state = %+v", st)
	}
	if len(st.Problems) != 1 || !strings.Contains(st.Problems[0], "daemon install") {
		t.Errorf("not installed should be the only problem, got %q", st.Problems)
	}
}

func TestParseLaunchctlPrint(t *testing.T) {
	state, lastExit, runs := daemon.ParseLaunchctlPrint([]byte(launchctlPrintFailed))
	if state != "not running" || lastExit != 78 || runs != 3 {
		t.Errorf("got state=%q lastExit=%d runs=%d, want not running/78/3", state, lastExit, runs)
	}
	never := strings.Replace(launchctlPrintFailed, "78: EX_CONFIG", "(never exited)", 1)
	if _, lastExit, _ := daemon.ParseLaunchctlPrint([]byte(never)); lastExit != daemon.NeverExited {
		t.Errorf("never exited: lastExit = %d", lastExit)
	}
}

func TestNextDailyRun(t *testing.T) {
	loc := time.FixedZone("X", 2*3600)
	before := time.Date(2026, 10, 2, 1, 0, 0, 0, loc)
	if got := daemon.NextDailyRun(before, 3, 0); !got.Equal(time.Date(2026, 10, 2, 3, 0, 0, 0, loc)) {
		t.Errorf("before the time: %v", got)
	}
	at := time.Date(2026, 10, 2, 3, 0, 0, 0, loc)
	if got := daemon.NextDailyRun(at, 3, 0); !got.Equal(time.Date(2026, 10, 3, 3, 0, 0, 0, loc)) {
		t.Errorf("at the time: %v", got)
	}
}

func TestLaunchdState_FailedRun(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	info := daemon.PlistInfo{Program: "/opt/homebrew/bin/immich-backup", Hour: 3, Minute: 30}
	st := daemon.LaunchdState(true, true, true, []byte(launchctlPrintFailed), info, now, "no gui")
	if !st.Active || st.Runs != 3 || !st.LastFailed {
		t.Fatalf("state = %+v", st)
	}
	if !strings.Contains(st.NextRun, "2026-10-03 03:30") {
		t.Errorf("NextRun = %q, want tomorrow 03:30", st.NextRun)
	}
	if p := strings.Join(st.Problems, "\n"); !strings.Contains(p, "EX_CONFIG") {
		t.Errorf("exit 78 should be explained, got %q", p)
	}
}

func TestLaunchdState_NotLoadedWithoutGUI(t *testing.T) {
	st := daemon.LaunchdState(true, false, false, nil, daemon.PlistInfo{Hour: -1, Minute: -1}, time.Now(), "needs a GUI session")
	if st.Active || len(st.Problems) != 1 || st.Problems[0] != "needs a GUI session" {
		t.Errorf("state = %+v", st)
	}
}

func TestParseSystemdUnit_RoundTrip(t *testing.T) {
	cfg := &config.Config{
		Backup: config.BackupConfig{Schedule: "0 3 * * *"},
		Daemon: config.DaemonConfig{LogPath: "/home/al ice/100%/daemon.log"},
	}
	bin := `/opt/my "apps"/100%/immich-backup`
	def := daemon.ParseSystemdUnit([]byte(daemon.GenerateSystemdUnit(bin, cfg, testEnv)))
	if def.BinaryPath != bin {
		t.Errorf("BinaryPath = %q, want %q", def.BinaryPath, bin)
	}
	if def.LogPath != cfg.Daemon.LogPath {
		t.Errorf("LogPath = %q, want %q", def.LogPath, cfg.Daemon.LogPath)
	}
}

func TestParseSystemdUnit_LegacyUnquotedExecStart(t *testing.T) {
	unit := "[Service]\nExecStart=/home/linuxbrew/.linuxbrew/Cellar/immich-backup/1.0/bin/immich-backup backup\n" +
		"StandardOutput=append:/home/u/.immich-backup/logs/daemon.log\n"
	def := daemon.ParseSystemdUnit([]byte(unit))
	if def.BinaryPath != "/home/linuxbrew/.linuxbrew/Cellar/immich-backup/1.0/bin/immich-backup" {
		t.Errorf("BinaryPath = %q", def.BinaryPath)
	}
	if def.LogPath != "/home/u/.immich-backup/logs/daemon.log" {
		t.Errorf("LogPath = %q", def.LogPath)
	}
}

func TestParsePlist_RoundTrip(t *testing.T) {
	cfg := &config.Config{
		Backup: config.BackupConfig{Schedule: "15 4 * * *"},
		Daemon: config.DaemonConfig{LogPath: "/Users/a&b/logs/daemon.log"},
	}
	plist, err := daemon.GeneratePlist("/Applications/A&B/immich-backup", cfg, testEnv)
	if err != nil {
		t.Fatal(err)
	}
	info, err := daemon.ParsePlist([]byte(plist))
	if err != nil {
		t.Fatalf("ParsePlist: %v", err)
	}
	if info.Program != "/Applications/A&B/immich-backup" || info.LogPath != cfg.Daemon.LogPath {
		t.Errorf("info = %+v", info)
	}
	if info.Hour != 4 || info.Minute != 15 {
		t.Errorf("schedule = %d:%d, want 4:15", info.Hour, info.Minute)
	}
}

func TestReport_MissingProgramIsAProblem(t *testing.T) {
	st := daemon.State{Installed: true, Active: true, Status: "active (waiting), enabled", Runs: -1}
	def := &daemon.Definition{Path: "/u/immich-backup.service", BinaryPath: filepath.Join(t.TempDir(), "gone")}
	text, err := daemon.Report(st, def)
	if err == nil || !strings.Contains(err.Error(), "daemon install") {
		t.Fatalf("missing program should be reported with the remedy, got %v", err)
	}
	if !strings.Contains(text, "Program:") || !strings.Contains(text, "active (waiting)") {
		t.Errorf("text should still describe the service:\n%s", text)
	}
}

// writeSystemdUnits writes an installed unit (with logPath) and timer to a
// temp dir and points m at them.
func writeSystemdUnits(t *testing.T, m *daemon.SystemdManager, logPath string) {
	t.Helper()
	dir := t.TempDir()
	unit, timer := filepath.Join(dir, "immich-backup.service"), filepath.Join(dir, "immich-backup.timer")
	bin, _ := os.Executable()
	cfg := &config.Config{Daemon: config.DaemonConfig{LogPath: logPath}}
	if err := os.WriteFile(unit, []byte(daemon.GenerateSystemdUnit(bin, cfg, testEnv)), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(timer, []byte("[Timer]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	m.SetPaths(unit, timer)
}

func TestSystemdStatus_ReportsScheduleAndLastRun(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{
		timerShowCmd:   {{out: timerShowActive}},
		serviceShowCmd: {{out: serviceShowOK}},
		lingerShow:     {{out: "yes\n"}},
	}}
	m := newTestSystemd(r, 1000, "/run/user/1000")
	writeSystemdUnits(t, m, "/home/alice/.immich-backup/logs/daemon.log")
	text, err := m.Status()
	if err != nil {
		t.Fatalf("Status: %v\n%s", err, text)
	}
	for _, want := range []string{"Next run:", "Sat 2026-10-03 03:00:00 CEST", "succeeded", "Linger:", "yes", "daemon.log"} {
		if !strings.Contains(text, want) {
			t.Errorf("status text missing %q:\n%s", want, text)
		}
	}
}

func TestSystemdStatus_FailedRunReturnsTextAndError(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{
		timerShowCmd:   {{out: timerShowActive}},
		serviceShowCmd: {{out: serviceShow209}},
		lingerShow:     {{out: "yes\n"}},
	}}
	m := newTestSystemd(r, 1000, "/run/user/1000")
	writeSystemdUnits(t, m, "/home/alice/.immich-backup/logs/daemon.log")
	text, err := m.Status()
	if err == nil || !strings.Contains(err.Error(), "209/STDOUT") {
		t.Fatalf("expected the 209 failure as the error, got %v", err)
	}
	if !strings.Contains(text, "Next run:") {
		t.Errorf("text should be returned with the error:\n%s", text)
	}
}

func TestSystemdStatus_LingerNotLoggedInMeansOff(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{
		timerShowCmd:   {{out: timerShowActive}},
		serviceShowCmd: {{out: serviceShowOK}},
		lingerShow:     {{out: "Failed to get user: User ID 1000 is not logged in or lingering", err: errors.New("exit status 1")}},
	}}
	m := newTestSystemd(r, 1000, "/run/user/1000")
	writeSystemdUnits(t, m, "/home/alice/.immich-backup/logs/daemon.log")
	_, err := m.Status()
	if err == nil || !strings.Contains(err.Error(), "sudo loginctl enable-linger alice") {
		t.Fatalf("expected the linger remedy, got %v", err)
	}
}

func TestSystemdStatus_NoUserBusIsError(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{
		timerShowCmd: {{out: "Failed to connect to bus: No medium found", err: errors.New("exit status 1")}},
	}}
	m := newTestSystemd(r, 1000, "/run/user/1000")
	_, err := m.Status()
	if err == nil || !strings.Contains(err.Error(), "Failed to connect to bus") {
		t.Fatalf("expected systemctl's error, got %v", err)
	}
}

func TestSystemdLogs_JournalAndLogTail(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "daemon.log")
	if err := os.WriteFile(logPath, []byte("level=INFO msg=\"backup complete\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{replies: map[string][]reply{
		journalCmd: {{out: "immich-backup.service: Failed with result 'exit-code'.\n"}},
	}}
	m := newTestSystemd(r, 1000, "/run/user/1000")
	writeSystemdUnits(t, m, logPath)
	out, err := m.Logs()
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	for _, want := range []string{"Failed with result 'exit-code'", "backup complete", logPath} {
		if !strings.Contains(out, want) {
			t.Errorf("logs missing %q:\n%s", want, out)
		}
	}
}

func TestSystemdLogs_JournalFailureStillShowsLogFile(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{
		journalCmd: {{out: "No journal files were found.", err: errors.New("exit status 1")}},
	}}
	m := newTestSystemd(r, 1000, "/run/user/1000")
	missing := filepath.Join(t.TempDir(), "logs", "daemon.log")
	writeSystemdUnits(t, m, missing)
	out, err := m.Logs()
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if !strings.Contains(out, "No journal files were found") || !strings.Contains(out, "no log file") {
		t.Errorf("logs should explain both sources:\n%s", out)
	}
}

// --quiet suppresses journalctl's "-- No entries --", so an empty journal is
// labelled rather than left blank.
func TestSystemdLogs_EmptyJournal(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{journalCmd: {{out: ""}}}}
	m := newTestSystemd(r, 1000, "/run/user/1000")
	writeSystemdUnits(t, m, filepath.Join(t.TempDir(), "daemon.log"))
	out, err := m.Logs()
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if !strings.Contains(out, "(no entries)") {
		t.Errorf("empty journal should be labelled:\n%s", out)
	}
}

func TestSystemdLogs_NotInstalled(t *testing.T) {
	m := newTestSystemd(&fakeRunner{}, 1000, "/run/user/1000")
	dir := t.TempDir()
	m.SetPaths(filepath.Join(dir, "u.service"), filepath.Join(dir, "u.timer"))
	if _, err := m.Logs(); err == nil || !strings.Contains(err.Error(), "daemon install") {
		t.Fatalf("expected a not-installed error, got %v", err)
	}
}

func TestLaunchdLogs_ShowsExitCodeAndLogTail(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{agentPrint: {{out: launchctlPrintFailed}}}}
	m, plist := newTestLaunchd(t, r, 501)
	logPath := filepath.Join(t.TempDir(), "daemon.log")
	if err := os.WriteFile(logPath, []byte("rclone not found\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Backup: config.BackupConfig{Schedule: "0 3 * * *"}, Daemon: config.DaemonConfig{LogPath: logPath}}
	if err := os.WriteFile(plist, []byte(mustPlist(t, "/opt/homebrew/bin/immich-backup", cfg)), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := m.Logs()
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	for _, want := range []string{"exit code 78", "rclone not found"} {
		if !strings.Contains(out, want) {
			t.Errorf("logs missing %q:\n%s", want, out)
		}
	}
}

func TestDetect_NoPanicOnUnsupportedOS(t *testing.T) {
	m, err := daemon.Detect()
	switch runtime.GOOS {
	case "linux", "darwin":
		if err != nil || m == nil {
			t.Errorf("Detect on %s: %v", runtime.GOOS, err)
		}
	default:
		if !errors.Is(err, daemon.ErrUnsupported) || m != nil {
			t.Errorf("Detect on %s should return ErrUnsupported, got %v", runtime.GOOS, err)
		}
	}
}
