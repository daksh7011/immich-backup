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

// LastRun holds the result of the most recent backup run.
type LastRun struct {
	Time   time.Time `json:"time"`
	Result string    `json:"result"` // ResultSuccess | ResultPartial | ResultError
	Error  string    `json:"error,omitempty"`
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
