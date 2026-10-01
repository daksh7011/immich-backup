package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/daksh7011/immich-backup/internal/daemon"
)

type fakeStateReader struct {
	st  daemon.State
	err error
}

func (f fakeStateReader) State() (daemon.State, error) { return f.st, f.err }

func TestServiceInfo_Unsupported(t *testing.T) {
	info := serviceInfo(nil, daemon.ErrUnsupported, "0 3 * * *")
	if !strings.Contains(info.State, "unavailable") || !strings.Contains(info.NextRun, "0 3 * * *") {
		t.Errorf("info = %+v", info)
	}
}

func TestServiceInfo_Active(t *testing.T) {
	st := daemon.State{Active: true, Status: "active (waiting), enabled", NextRun: "Sat 2026-10-03 03:00:00 CEST"}
	info := serviceInfo(fakeStateReader{st: st}, nil, "0 3 * * *")
	if info.State != st.Status || info.NextRun != st.NextRun || len(info.Problems) != 0 {
		t.Errorf("info = %+v", info)
	}
}

func TestServiceInfo_NotScheduled(t *testing.T) {
	st := daemon.State{Status: "not installed", Problems: []string{"not installed: run `immich-backup daemon install`"}}
	info := serviceInfo(fakeStateReader{st: st}, nil, "0 3 * * *")
	if !strings.HasPrefix(info.NextRun, "none") || len(info.Problems) != 1 {
		t.Errorf("info = %+v", info)
	}
}

func TestServiceInfo_QueryError(t *testing.T) {
	info := serviceInfo(fakeStateReader{err: errors.New("Failed to connect to bus")}, nil, "0 3 * * *")
	if info.State != "unknown" || len(info.Problems) != 1 || !strings.Contains(info.Problems[0], "bus") {
		t.Errorf("info = %+v", info)
	}
}
