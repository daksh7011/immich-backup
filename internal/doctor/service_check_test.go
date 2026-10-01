package doctor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daksh7011/immich-backup/internal/daemon"
)

// fakeService is a daemon.Manager stand-in for the service checks.
type fakeService struct {
	st     daemon.State
	stErr  error
	def    *daemon.Definition
	defErr error
}

func (f fakeService) State() (daemon.State, error)            { return f.st, f.stErr }
func (f fakeService) Definition() (*daemon.Definition, error) { return f.def, f.defErr }

func TestAnyFailed_IgnoresWarnings(t *testing.T) {
	results := []CheckResult{
		{Name: "Config", OK: true},
		{Name: "Daemon Linger", Warn: true, Message: "lingering is no"},
	}
	if AnyFailed(results) {
		t.Error("warnings must not make AnyFailed true, or backup would be blocked")
	}
	if !AnyFailed(append(results, CheckResult{Name: "rclone Binary"})) {
		t.Error("a real failure must still make AnyFailed true")
	}
}

func TestServiceChecks_LingerOnlyOnLinux(t *testing.T) {
	svc := fakeService{}
	names := func(cs []namedCheck) (out []string) {
		for _, c := range cs {
			out = append(out, c.name)
		}
		return out
	}
	if got := names(serviceChecks(svc, "linux")); strings.Join(got, ",") != "Daemon Program,Daemon Linger" {
		t.Errorf("linux checks = %v", got)
	}
	if got := names(serviceChecks(svc, "darwin")); strings.Join(got, ",") != "Daemon Program" {
		t.Errorf("darwin checks = %v", got)
	}
}

func TestCheckServiceProgram_Unsupported(t *testing.T) {
	r := checkServiceProgram(nil, "windows")
	if r.OK || !r.Warn || !strings.Contains(r.Message, "not supported on windows") {
		t.Errorf("result = %+v", r)
	}
}

func TestCheckServiceProgram_NotInstalled(t *testing.T) {
	r := checkServiceProgram(fakeService{}, "linux")
	if r.OK || !r.Warn || !strings.Contains(r.Remedy, "daemon install") {
		t.Errorf("result = %+v", r)
	}
}

func TestCheckServiceProgram_MissingBinary(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "Cellar", "immich-backup", "1.0", "bin", "immich-backup")
	r := checkServiceProgram(fakeService{def: &daemon.Definition{Path: "/u/immich-backup.service", BinaryPath: gone}}, "linux")
	if r.OK || !r.Warn || !strings.Contains(r.Message, gone) || !strings.Contains(r.Remedy, "daemon install") {
		t.Errorf("result = %+v", r)
	}
}

func TestCheckServiceProgram_OK(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r := checkServiceProgram(fakeService{def: &daemon.Definition{BinaryPath: bin}}, "linux")
	if !r.OK {
		t.Errorf("existing program should pass: %+v", r)
	}
}

func TestCheckServiceProgram_DefinitionError(t *testing.T) {
	r := checkServiceProgram(fakeService{defErr: errors.New("read unit file: permission denied")}, "linux")
	if r.OK || !r.Warn || !strings.Contains(r.Message, "permission denied") {
		t.Errorf("result = %+v", r)
	}
}

func TestCheckServiceLinger(t *testing.T) {
	on := checkServiceLinger(fakeService{st: daemon.State{Linger: "yes", User: "alice"}})
	if !on.OK {
		t.Errorf("linger yes should pass: %+v", on)
	}
	off := checkServiceLinger(fakeService{st: daemon.State{Linger: "no", User: "alice"}})
	if off.OK || !off.Warn || !strings.Contains(off.Remedy, "sudo loginctl enable-linger alice") {
		t.Errorf("linger no should warn with the sudo remedy: %+v", off)
	}
	failed := checkServiceLinger(fakeService{stErr: errors.New("Failed to connect to bus")})
	if failed.OK || !failed.Warn || !strings.Contains(failed.Message, "Failed to connect to bus") {
		t.Errorf("query failure should warn: %+v", failed)
	}
}
