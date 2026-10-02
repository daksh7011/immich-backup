// internal/status/status.go
package status

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Values of LastRun.Result. Persisted in last-run.json — do not change.
const (
	ResultSuccess = "success"
	ResultPartial = "partial" // media sync finished but rclone skipped some files
	ResultError   = "error"
)

// LastRun holds the result of the most recent backup attempt, including
// attempts that failed before any data was copied (config, prerequisites).
type LastRun struct {
	Time   time.Time `json:"time"`   // when the attempt started
	Result string    `json:"result"` // ResultSuccess | ResultPartial | ResultError
	Error  string    `json:"error,omitempty"`
	// LastSuccess is when the most recent successful attempt started. It is
	// carried forward by Record so a failing run does not hide the last good backup.
	LastSuccess time.Time `json:"last_success,omitzero"`
}

// Load reads the last-run status from path.
func Load(path string) (*LastRun, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read status: %w", err)
	}
	var r LastRun
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse status: %w", err)
	}
	return &r, nil
}

// Save writes r as indented JSON to path, creating parent directories as needed.
func Save(path string, r *LastRun) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal status: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// Record saves r as the latest attempt. A success sets r.LastSuccess to r.Time;
// any other result keeps LastSuccess from the record already at path.
func Record(path string, r *LastRun) error {
	if r.Result == ResultSuccess {
		r.LastSuccess = r.Time
		return Save(path, r)
	}
	// A missing or unreadable previous record just means no known success.
	if prev, err := Load(path); err == nil {
		r.LastSuccess = prev.LastSuccess
		// Records written before last_success existed only carry time+result.
		if prev.Result == ResultSuccess && prev.Time.After(r.LastSuccess) {
			r.LastSuccess = prev.Time
		}
	}
	return Save(path, r)
}
