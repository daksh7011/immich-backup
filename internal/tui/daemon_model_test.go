package tui

import (
	"errors"
	"strings"
	"testing"
)

func daemonResult(t *testing.T, msg DaemonResultMsg) (DaemonModel, string) {
	t.Helper()
	m, _ := NewDaemonModel(make(chan any), "Fetching service status…").Update(msg)
	dm := m.(DaemonModel)
	return dm, dm.View().Content
}

func TestDaemonModel_ShowsMsgWhenErrSet(t *testing.T) {
	status := "Service:   active (waiting), enabled\nNext run:  Sat 2026-10-03 03:00:00 CEST\n"
	dm, out := daemonResult(t, DaemonResultMsg{Msg: status, Err: errors.New("the last scheduled backup failed (exit-code, status 209)")})
	if dm.Err() == nil {
		t.Fatal("Err() should return the error")
	}
	for _, want := range []string{"status 209", "active (waiting), enabled", "Sat 2026-10-03 03:00:00 CEST"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
}

func TestDaemonModel_SingleLineMsgIsDetail(t *testing.T) {
	dm, out := daemonResult(t, DaemonResultMsg{Msg: "Done."})
	if dm.Err() != nil || !strings.Contains(out, "Done.") {
		t.Errorf("view should show Done.:\n%s", out)
	}
}

func TestDaemonModel_ErrorOnly(t *testing.T) {
	_, out := daemonResult(t, DaemonResultMsg{Err: errors.New("systemctl --user start x: exit status 1")})
	if !strings.Contains(out, "exit status 1") {
		t.Errorf("view should show the error:\n%s", out)
	}
}
