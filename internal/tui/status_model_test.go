package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/daksh7011/immich-backup/internal/status"
)

func TestStatusModel_FailedRunShowsLastSuccess(t *testing.T) {
	run := &status.LastRun{
		Time:        time.Date(2026, 10, 2, 3, 0, 0, 0, time.UTC),
		Result:      status.ResultError,
		Error:       "prerequisite checks failed: rclone Binary: not found",
		LastSuccess: time.Date(2026, 9, 30, 3, 0, 0, 0, time.UTC),
	}
	out := NewStatusModel(run, "(schedule: 0 3 * * *)").View().Content
	for _, want := range []string{"[error]", "rclone Binary: not found", "Last ok:", "2026-09-30 03:00:00"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
}

func TestStatusModel_FailedRunWithoutSuccess(t *testing.T) {
	run := &status.LastRun{Time: time.Now().UTC(), Result: status.ResultError, Error: "boom"}
	out := NewStatusModel(run, "").View().Content
	if !strings.Contains(out, "never recorded") {
		t.Errorf("view should say no success is recorded:\n%s", out)
	}
}

func TestStatusModel_SuccessHidesLastOK(t *testing.T) {
	at := time.Now().UTC()
	run := &status.LastRun{Time: at, Result: status.ResultSuccess, LastSuccess: at}
	out := NewStatusModel(run, "").View().Content
	if strings.Contains(out, "Last ok:") {
		t.Errorf("successful run should not repeat its time as Last ok:\n%s", out)
	}
}
