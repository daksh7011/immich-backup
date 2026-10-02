package tui

import (
	"strings"
	"testing"

	"github.com/daksh7011/immich-backup/internal/doctor"
)

func runDoctorModel(results ...doctor.CheckResult) DoctorModel {
	var m DoctorModel = NewDoctorModel(make(chan any))
	for _, r := range results {
		next, _ := m.Update(doctor.CheckStartMsg{Name: r.Name})
		next, _ = next.(DoctorModel).Update(r)
		m = next.(DoctorModel)
	}
	next, _ := m.Update(chanClosedMsg{})
	return next.(DoctorModel)
}

func TestDoctorModel_WarningDoesNotFail(t *testing.T) {
	m := runDoctorModel(
		doctor.CheckResult{Name: "Config", OK: true, Message: "config is valid"},
		doctor.CheckResult{Name: "Daemon Linger", Warn: true, Message: "lingering is no", Remedy: "Run `sudo loginctl enable-linger alice`"},
	)
	if m.AnyFailed() {
		t.Error("a warning must not make doctor fail")
	}
	out := m.View().Content
	for _, want := range []string{"1/2 checks passed, 1 warning(s)", "enable-linger alice"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
}

func TestDoctorModel_FailureStillFails(t *testing.T) {
	m := runDoctorModel(
		doctor.CheckResult{Name: "rclone Binary", Message: "not found"},
		doctor.CheckResult{Name: "Daemon Program", Warn: true, Message: "not installed"},
	)
	if !m.AnyFailed() {
		t.Error("a failed check must still fail doctor")
	}
}
