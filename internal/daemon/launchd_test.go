package daemon_test

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/daksh7011/immich-backup/internal/daemon"
)

const (
	guiDomain    = "launchctl print gui/501"
	agentTarget  = "gui/501/com.immich-backup.agent"
	agentPrint   = "launchctl print " + agentTarget
	agentBootout = "launchctl bootout " + agentTarget
	agentEnable  = "launchctl enable " + agentTarget
)

// notLoaded is launchctl's answer for a service that is not in the domain.
var notLoaded = reply{out: "Boot-out failed: 3: No such process", err: errors.New("exit status 3")}

// newTestLaunchd returns a launchd manager for uid 501 whose plist is an
// existing file in a temp dir, plus that path.
func newTestLaunchd(t *testing.T, r *fakeRunner, uid int) (*daemon.LaunchdManager, string) {
	t.Helper()
	plist := filepath.Join(t.TempDir(), "com.immich-backup.agent.plist")
	if err := os.WriteFile(plist, []byte("<plist/>"), 0644); err != nil {
		t.Fatal(err)
	}
	return daemon.NewLaunchdManagerWith(r, func() int { return uid }, func() string { return plist }), plist
}

func assertCalls(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestLaunchdActivate_CallSequence(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{}}
	m, plist := newTestLaunchd(t, r, 501)
	if err := m.Activate(); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	assertCalls(t, r.calls,
		agentBootout,
		agentEnable,
		"launchctl bootstrap gui/501 "+plist,
		agentPrint,
	)
}

func TestLaunchdActivate_IgnoresNotLoadedOnBootout(t *testing.T) {
	for _, nl := range []reply{
		notLoaded,
		{out: "Boot-out failed: 113: Could not find specified service", err: errors.New("exit status 113")},
	} {
		r := &fakeRunner{replies: map[string][]reply{agentBootout: {nl}}}
		m, _ := newTestLaunchd(t, r, 501)
		if err := m.Activate(); err != nil {
			t.Errorf("bootout %q should be ignored, got: %v", nl.out, err)
		}
		if len(r.calls) != 4 {
			t.Errorf("expected all four steps to run, calls: %v", r.calls)
		}
	}
}

func TestLaunchdActivate_BootoutOtherFailureIsReturned(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{
		agentBootout: {{out: "Boot-out failed: 1: Operation not permitted", err: errors.New("exit status 1")}},
	}}
	m, _ := newTestLaunchd(t, r, 501)
	err := m.Activate()
	if err == nil || !strings.Contains(err.Error(), "Operation not permitted") {
		t.Fatalf("expected bootout failure with launchctl output, got: %v", err)
	}
	if len(r.calls) != 1 {
		t.Errorf("should stop after bootout fails, calls: %v", r.calls)
	}
}

func TestLaunchdActivate_BootstrapFailureIncludesOutput(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{}}
	m, plist := newTestLaunchd(t, r, 501)
	r.replies["launchctl bootstrap gui/501 "+plist] = []reply{
		{out: "Bootstrap failed: 5: Input/output error", err: errors.New("exit status 5")},
	}
	err := m.Activate()
	if err == nil {
		t.Fatal("expected error when bootstrap fails")
	}
	for _, want := range []string{"bootstrap", "Input/output error"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should contain %q, got: %v", want, err)
		}
	}
	if len(r.calls) != 3 {
		t.Errorf("should stop after bootstrap fails, calls: %v", r.calls)
	}
}

func TestLaunchdActivate_FailsWhenNotLoadedAfterBootstrap(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{
		agentPrint: {{out: "Could not find service \"com.immich-backup.agent\" in domain for port", err: errors.New("exit status 113")}},
	}}
	m, _ := newTestLaunchd(t, r, 501)
	err := m.Activate()
	if err == nil || !strings.Contains(err.Error(), "not loaded") {
		t.Fatalf("expected a not-loaded error after bootstrap, got: %v", err)
	}
}

func TestLaunchdInstall_NoGUISessionFailsBeforeWriting(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{
		guiDomain: {{out: "Could not find domain for port identifier", err: errors.New("exit status 113")}},
	}}
	m, plist := newTestLaunchd(t, r, 501)
	if err := os.Remove(plist); err != nil {
		t.Fatal(err)
	}
	err := m.Install(testCfg)
	if err == nil {
		t.Fatal("expected error without a GUI session")
	}
	if !strings.Contains(err.Error(), "logged-in GUI session") || !strings.Contains(err.Error(), "auto-login") {
		t.Errorf("error should explain the GUI session requirement, got: %v", err)
	}
	assertCalls(t, r.calls, guiDomain)
	if _, err := os.Stat(plist); !os.IsNotExist(err) {
		t.Errorf("no plist should be written without a GUI session, stat err: %v", err)
	}
}

// exitErr is a failed command whose error carries only an exit status, like
// *exec.ExitError when launchctl prints nothing.
type exitErr int

func (e exitErr) Error() string { return "exit status " + strconv.Itoa(int(e)) }
func (e exitErr) ExitCode() int { return int(e) }

func TestLaunchdActivate_NotLoadedByExitCodeAlone(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{agentBootout: {{err: exitErr(113)}}}}
	m, _ := newTestLaunchd(t, r, 501)
	if err := m.Activate(); err != nil {
		t.Fatalf("bootout exit 113 with no output should be ignored, got: %v", err)
	}
	if len(r.calls) != 4 {
		t.Errorf("expected all four steps to run, calls: %v", r.calls)
	}
}

func TestLaunchdActivate_WaitsForRunningJobToUnload(t *testing.T) {
	for _, inProgress := range []reply{
		{out: "Boot-out failed: 36: Operation now in progress", err: errors.New("exit status 36")},
		{err: exitErr(36)},
	} {
		r := &fakeRunner{replies: map[string][]reply{
			agentBootout: {inProgress},
			agentPrint:   {{out: "state = running"}, {out: "state = running"}, notLoaded, {out: "state = waiting"}},
		}}
		m, plist := newTestLaunchd(t, r, 501)
		if err := m.Activate(); err != nil {
			t.Fatalf("Activate (bootout %v): %v", inProgress.err, err)
		}
		assertCalls(t, r.calls,
			agentBootout,
			agentPrint,
			agentPrint,
			agentPrint,
			agentEnable,
			"launchctl bootstrap gui/501 "+plist,
			agentPrint,
		)
	}
}

func TestLaunchdActivate_FailsWhenJobStaysLoadedAfterBootout(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{
		agentBootout: {{out: "Boot-out failed: 36: Operation now in progress", err: errors.New("exit status 36")}},
		agentPrint:   {{out: "state = running"}},
	}}
	m, _ := newTestLaunchd(t, r, 501)
	err := m.Activate()
	if err == nil || !strings.Contains(err.Error(), "still loaded") {
		t.Fatalf("expected a still-loaded error, got: %v", err)
	}
	for _, c := range r.calls {
		if strings.Contains(c, "bootstrap") || strings.Contains(c, "enable") {
			t.Errorf("must not bootstrap while the old job is loaded: %s", c)
		}
	}
	if n := len(r.calls); n < 2 || n > 40 {
		t.Errorf("polling should be bounded, got %d calls", n)
	}
}

func TestLaunchdUninstall_WaitFailureKeepsPlist(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{
		agentBootout: {{out: "Boot-out failed: 36: Operation now in progress", err: errors.New("exit status 36")}},
		agentPrint:   {{out: "state = running"}},
	}}
	m, plist := newTestLaunchd(t, r, 501)
	if err := m.Uninstall(); err == nil {
		t.Fatal("expected an error while the job is still loaded")
	}
	if _, err := os.Stat(plist); err != nil {
		t.Errorf("plist should be kept while the job is still loaded: %v", err)
	}
}

func TestLaunchdStatus_NoGUISession(t *testing.T) {
	noDomain := reply{out: "Could not find domain for port identifier", err: errors.New("exit status 113")}
	r := &fakeRunner{replies: map[string][]reply{agentPrint: {noDomain}, guiDomain: {noDomain}}}
	m, _ := newTestLaunchd(t, r, 501)
	_, err := m.Status()
	if err == nil || !strings.Contains(err.Error(), "logged-in GUI session") {
		t.Fatalf("expected the GUI session explanation, got: %v", err)
	}
	if strings.Contains(err.Error(), "daemon install") {
		t.Errorf("should not suggest daemon install without a GUI session: %v", err)
	}
}

func TestLaunchdInstall_RejectsRoot(t *testing.T) {
	r := &fakeRunner{}
	m, _ := newTestLaunchd(t, r, 0)
	err := m.Install(testCfg)
	if err == nil || !strings.Contains(err.Error(), "without sudo") {
		t.Fatalf("expected root to be rejected, got: %v", err)
	}
	if len(r.calls) != 0 {
		t.Errorf("no launchctl calls expected as root, got: %v", r.calls)
	}
}

func TestLaunchdUninstall_BootsOutAndRemovesPlist(t *testing.T) {
	for _, bootout := range []reply{{}, notLoaded} {
		r := &fakeRunner{replies: map[string][]reply{agentBootout: {bootout}}}
		m, plist := newTestLaunchd(t, r, 501)
		if err := m.Uninstall(); err != nil {
			t.Fatalf("Uninstall (bootout %q): %v", bootout.out, err)
		}
		assertCalls(t, r.calls, guiDomain, agentBootout)
		if _, err := os.Stat(plist); !os.IsNotExist(err) {
			t.Errorf("plist should be removed, stat err: %v", err)
		}
	}
}

func TestLaunchdUninstall_BootoutFailureKeepsPlist(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{
		agentBootout: {{out: "Boot-out failed: 1: Operation not permitted", err: errors.New("exit status 1")}},
	}}
	m, plist := newTestLaunchd(t, r, 501)
	if err := m.Uninstall(); err == nil || !strings.Contains(err.Error(), "Operation not permitted") {
		t.Fatalf("expected bootout failure, got: %v", err)
	}
	if _, err := os.Stat(plist); err != nil {
		t.Errorf("plist should be kept while the job may still be loaded: %v", err)
	}
}

func TestLaunchdUninstall_MissingPlistIsFine(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{agentBootout: {notLoaded}}}
	m, plist := newTestLaunchd(t, r, 501)
	_ = os.Remove(plist)
	if err := m.Uninstall(); err != nil {
		t.Errorf("Uninstall without a plist: %v", err)
	}
}

func TestLaunchdStop_BootsOut(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{agentBootout: {notLoaded}}}
	m, plist := newTestLaunchd(t, r, 501)
	if err := m.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	assertCalls(t, r.calls, guiDomain, agentBootout)
	if _, err := os.Stat(plist); err != nil {
		t.Errorf("Stop must keep the plist: %v", err)
	}
}

func TestLaunchdStart_BootstrapsWhenNotLoaded(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{
		agentPrint: {{out: "Could not find service", err: errors.New("exit status 113")}, {out: "state = waiting"}},
	}}
	m, plist := newTestLaunchd(t, r, 501)
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	assertCalls(t, r.calls,
		guiDomain,
		agentPrint,
		agentEnable,
		"launchctl bootstrap gui/501 "+plist,
		agentPrint,
	)
	for _, c := range r.calls {
		if strings.Contains(c, "kickstart") || strings.Contains(c, "launchctl start") {
			t.Errorf("Start must not run a backup now: %s", c)
		}
	}
}

func TestLaunchdStart_NoopWhenLoaded(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{agentPrint: {{out: "state = waiting"}}}}
	m, _ := newTestLaunchd(t, r, 501)
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	assertCalls(t, r.calls, guiDomain, agentPrint)
}

func TestLaunchdStart_NotInstalled(t *testing.T) {
	r := &fakeRunner{}
	m, plist := newTestLaunchd(t, r, 501)
	_ = os.Remove(plist)
	err := m.Start()
	if err == nil || !strings.Contains(err.Error(), "daemon install") {
		t.Fatalf("expected a not-installed error naming daemon install, got: %v", err)
	}
}

func TestLaunchdRestart_BootsOutThenBootstraps(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{}}
	m, plist := newTestLaunchd(t, r, 501)
	if err := m.Restart(); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	assertCalls(t, r.calls,
		guiDomain,
		agentBootout,
		agentEnable,
		"launchctl bootstrap gui/501 "+plist,
		agentPrint,
	)
}

func TestLaunchdStatus_PrintsService(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{agentPrint: {{out: "state = waiting\n"}}}}
	m, _ := newTestLaunchd(t, r, 501)
	out, err := m.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !strings.Contains(out, "state = waiting") {
		t.Errorf("Status should return launchctl print output, got %q", out)
	}
	assertCalls(t, r.calls, agentPrint)
}

func TestLaunchdStatus_NotLoaded(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{
		agentPrint: {{out: "Could not find service", err: errors.New("exit status 113")}},
	}}
	m, _ := newTestLaunchd(t, r, 501)
	_, err := m.Status()
	if err == nil || !strings.Contains(err.Error(), "not loaded") || !strings.Contains(err.Error(), "daemon install") {
		t.Fatalf("expected a not-loaded error naming daemon install, got: %v", err)
	}
}

func TestLaunchdUninstall_NoGUISessionSkipsBootout(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{
		guiDomain: {{out: "Could not find domain for port identifier", err: errors.New("exit status 113")}},
	}}
	m, plist := newTestLaunchd(t, r, 501)
	if err := m.Uninstall(); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	assertCalls(t, r.calls, guiDomain)
	if _, err := os.Stat(plist); !os.IsNotExist(err) {
		t.Errorf("plist should be removed, stat err: %v", err)
	}
}
