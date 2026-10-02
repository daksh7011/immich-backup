// internal/status/status_test.go
package status_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daksh7011/immich-backup/internal/status"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "last-run.json")
	want := &status.LastRun{
		Time:   time.Now().UTC().Truncate(time.Second),
		Result: "success",
	}
	if err := status.Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := status.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !got.Time.Equal(want.Time) {
		t.Errorf("time: got %v, want %v", got.Time, want.Time)
	}
	if got.Result != want.Result {
		t.Errorf("result: got %q, want %q", got.Result, want.Result)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := status.Load("/nonexistent/last-run.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestRoundTrip_WithError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "last-run.json")
	want := &status.LastRun{
		Time:   time.Now().UTC().Truncate(time.Second),
		Result: "error",
		Error:  "rclone: exit status 1",
	}
	if err := status.Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := status.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Error != want.Error {
		t.Errorf("error field: got %q, want %q", got.Error, want.Error)
	}
}

func TestRecord_SuccessSetsLastSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "last-run.json")
	at := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	if err := status.Record(path, &status.LastRun{Time: at, Result: status.ResultSuccess}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	got, err := status.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !got.LastSuccess.Equal(at) {
		t.Errorf("LastSuccess: got %v, want %v", got.LastSuccess, at)
	}
}

func TestRecord_FailureKeepsLastSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "last-run.json")
	ok := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	failed := ok.Add(24 * time.Hour)
	if err := status.Record(path, &status.LastRun{Time: ok, Result: status.ResultSuccess}); err != nil {
		t.Fatalf("Record success: %v", err)
	}
	if err := status.Record(path, &status.LastRun{Time: failed, Result: status.ResultError, Error: "rclone Binary: not found"}); err != nil {
		t.Fatalf("Record error: %v", err)
	}
	// A second failure must not lose the last success either.
	if err := status.Record(path, &status.LastRun{Time: failed.Add(time.Hour), Result: status.ResultPartial, Error: "x"}); err != nil {
		t.Fatalf("Record partial: %v", err)
	}
	got, err := status.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Result != status.ResultPartial || !got.Time.Equal(failed.Add(time.Hour)) {
		t.Errorf("latest attempt: got %q at %v", got.Result, got.Time)
	}
	if !got.LastSuccess.Equal(ok) {
		t.Errorf("LastSuccess: got %v, want %v", got.LastSuccess, ok)
	}
}

func TestRecord_FailureAfterLegacySuccessRecord(t *testing.T) {
	// Records written before last_success existed only have time+result.
	path := filepath.Join(t.TempDir(), "last-run.json")
	ok := time.Date(2026, 9, 30, 3, 0, 0, 0, time.UTC)
	if err := os.WriteFile(path, []byte(`{"time":"2026-09-30T03:00:00Z","result":"success"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := status.Record(path, &status.LastRun{Time: ok.Add(24 * time.Hour), Result: status.ResultError, Error: "boom"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	got, err := status.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !got.LastSuccess.Equal(ok) {
		t.Errorf("LastSuccess: got %v, want %v", got.LastSuccess, ok)
	}
}

func TestRecord_FailureWithNoHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "last-run.json")
	if err := status.Record(path, &status.LastRun{Time: time.Now().UTC(), Result: status.ResultError, Error: "boom"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	got, err := status.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !got.LastSuccess.IsZero() {
		t.Errorf("LastSuccess: got %v, want zero", got.LastSuccess)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "last_success") {
		t.Errorf("zero last_success should be omitted:\n%s", data)
	}
}
