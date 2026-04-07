// internal/status/status_test.go
package status_test

import (
	"path/filepath"
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
