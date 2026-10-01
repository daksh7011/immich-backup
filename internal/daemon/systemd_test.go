package daemon_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/daksh7011/immich-backup/internal/daemon"
)

// fakeRunner records every command and answers from a script keyed by the
// full command line. Unscripted commands succeed with no output.
type fakeRunner struct {
	calls   []string
	replies map[string][]reply // consumed in order; the last one repeats
}

type reply struct {
	out string
	err error
}

func (f *fakeRunner) Run(name string, args ...string) ([]byte, error) {
	cmd := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, cmd)
	rs := f.replies[cmd]
	if len(rs) == 0 {
		return nil, nil
	}
	r := rs[0]
	if len(rs) > 1 {
		f.replies[cmd] = rs[1:]
	}
	return []byte(r.out), r.err
}

const (
	lingerShow   = "loginctl show-user alice -p Linger --value"
	lingerEnable = "loginctl enable-linger alice"
)

func newTestSystemd(r *fakeRunner, euid int, xdg string) *daemon.SystemdManager {
	getenv := func(k string) string {
		if k == "XDG_RUNTIME_DIR" {
			return xdg
		}
		return ""
	}
	return daemon.NewSystemdManagerWith(r, func() int { return euid }, getenv,
		func() (string, error) { return "alice", nil })
}

func TestGenerateSystemdTimer_NoServiceDependency(t *testing.T) {
	timer, err := daemon.GenerateSystemdTimer("0 3 * * *")
	if err != nil {
		t.Fatalf("GenerateSystemdTimer: %v", err)
	}
	for _, bad := range []string{"Requires=", "BindsTo=", "After=network.target"} {
		if strings.Contains(timer, bad) {
			t.Errorf("timer must not contain %q (starting the timer would run a backup):\n%s", bad, timer)
		}
	}
}

func TestGenerateSystemdUnit_NoNetworkTargetOrdering(t *testing.T) {
	unit := daemon.GenerateSystemdUnit("/usr/local/bin/immich-backup", testCfg, testEnv)
	if strings.Contains(unit, "After=network.target") {
		t.Errorf("user unit must not order after network.target (no-op in a user manager):\n%s", unit)
	}
}

func TestParseLinger(t *testing.T) {
	cases := map[string]bool{
		"yes\n":        true,
		"yes":          true,
		"Linger=yes\n": true,
		"no\n":         false,
		"Linger=no":    false,
		"":             false,
	}
	for in, want := range cases {
		if got := daemon.ParseLinger([]byte(in)); got != want {
			t.Errorf("ParseLinger(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestSystemdActivate_CallSequence(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{lingerShow: {{out: "yes\n"}}}}
	m := newTestSystemd(r, 1000, "/run/user/1000")
	if err := m.Activate(); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	want := []string{
		"systemctl --user daemon-reload",
		"systemctl --user enable immich-backup.timer",
		"systemctl --user restart immich-backup.timer",
		lingerShow,
	}
	if strings.Join(r.calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(r.calls, "\n"), strings.Join(want, "\n"))
	}
}

func TestSystemdActivate_DaemonReloadFailureIsReturned(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{
		"systemctl --user daemon-reload": {{out: "Failed to connect to bus: No medium found", err: errors.New("exit status 1")}},
	}}
	m := newTestSystemd(r, 1000, "/run/user/1000")
	err := m.Activate()
	if err == nil {
		t.Fatal("expected error when daemon-reload fails")
	}
	if !strings.Contains(err.Error(), "daemon-reload") || !strings.Contains(err.Error(), "Failed to connect to bus") {
		t.Errorf("error should name the command and include systemctl output, got: %v", err)
	}
	if len(r.calls) != 1 {
		t.Errorf("should stop after daemon-reload fails, calls: %v", r.calls)
	}
}

func TestSystemdActivate_EnablesLingerWhenOff(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{lingerShow: {{out: "no\n"}, {out: "yes\n"}}}}
	m := newTestSystemd(r, 1000, "/run/user/1000")
	if err := m.Activate(); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	tail := r.calls[len(r.calls)-3:]
	want := []string{lingerShow, lingerEnable, lingerShow}
	if strings.Join(tail, "\n") != strings.Join(want, "\n") {
		t.Errorf("linger calls:\n%s\nwant:\n%s", strings.Join(tail, "\n"), strings.Join(want, "\n"))
	}
}

func TestSystemdActivate_LingerEnableFailsGivesSudoRemedy(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{
		lingerShow:   {{out: "no\n"}},
		lingerEnable: {{out: "Could not enable linger: Access denied", err: errors.New("exit status 1")}},
	}}
	m := newTestSystemd(r, 1000, "/run/user/1000")
	err := m.Activate()
	if err == nil {
		t.Fatal("expected error when linger stays off")
	}
	msg := err.Error()
	for _, want := range []string{"sudo loginctl enable-linger alice", "Access denied"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error should contain %q, got: %v", want, msg)
		}
	}
}

func TestSystemdActivate_LingerStillOffAfterEnable(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{lingerShow: {{out: "no\n"}}}}
	m := newTestSystemd(r, 1000, "/run/user/1000")
	err := m.Activate()
	if err == nil || !strings.Contains(err.Error(), "sudo loginctl enable-linger alice") {
		t.Fatalf("expected sudo remedy when linger is still off, got: %v", err)
	}
}

func TestSystemdPreflight_RejectsRoot(t *testing.T) {
	m := newTestSystemd(&fakeRunner{}, 0, "/run/user/0")
	err := m.Preflight()
	if err == nil {
		t.Fatal("expected error when running as root")
	}
	if !strings.Contains(err.Error(), "root") {
		t.Errorf("error should mention root, got: %v", err)
	}
}

func TestSystemdPreflight_RequiresXDGRuntimeDir(t *testing.T) {
	m := newTestSystemd(&fakeRunner{}, 1000, "")
	err := m.Preflight()
	if err == nil {
		t.Fatal("expected error when XDG_RUNTIME_DIR is unset")
	}
	if !strings.Contains(err.Error(), "XDG_RUNTIME_DIR") || !strings.Contains(err.Error(), "/run/user/1000") {
		t.Errorf("error should name XDG_RUNTIME_DIR and the expected dir, got: %v", err)
	}
}

func TestSystemdPreflight_OK(t *testing.T) {
	m := newTestSystemd(&fakeRunner{}, 1000, "/run/user/1000")
	if err := m.Preflight(); err != nil {
		t.Errorf("Preflight: %v", err)
	}
}

func TestSystemdStart_SurfacesSystemctlOutput(t *testing.T) {
	r := &fakeRunner{replies: map[string][]reply{
		"systemctl --user start immich-backup.timer": {{out: "Unit immich-backup.timer not found.", err: errors.New("exit status 5")}},
	}}
	m := newTestSystemd(r, 1000, "/run/user/1000")
	err := m.Start()
	if err == nil || !strings.Contains(err.Error(), "Unit immich-backup.timer not found.") {
		t.Fatalf("expected systemctl output in error, got: %v", err)
	}
}

func TestSystemdStart_RejectsRootBeforeSystemctl(t *testing.T) {
	r := &fakeRunner{}
	m := newTestSystemd(r, 0, "/run/user/0")
	if err := m.Start(); err == nil {
		t.Fatal("expected error when running as root")
	}
	if len(r.calls) != 0 {
		t.Errorf("no command should run as root, calls: %v", r.calls)
	}
}
