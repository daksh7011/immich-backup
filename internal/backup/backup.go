// internal/backup/backup.go
package backup

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/daksh7011/immich-backup/internal/docker"
)

// Progress message types sent to the TUI channel during a backup run.
// These are defined here so internal/tui/backup_model.go can import them
// without creating an import cycle.
type ProgressMsg struct{ Text string }
type ErrorMsg struct{ Err error }
type DoneMsg struct{}

// ScanMsg is sent after rclone size --json completes.
type ScanMsg struct {
	TotalFiles int64
	TotalBytes int64
}

// MediaProgressMsg is sent each stats tick during rclone sync.
type MediaProgressMsg struct {
	TransferredBytes int64
	TotalBytes       int64
	Speed            float64 // bytes/sec; 0 on first tick
	ETA              *int64  // nil = not yet known (rclone emits null on first tick)
	FilesDone        int64
	FilesTotal       int64
}

// RcloneErrorMsg is sent when rclone reports a file-level error.
// Non-fatal: backup continues because RunMedia uses --ignore-errors.
type RcloneErrorMsg struct {
	Text string
}

// Private JSON structs used only inside this package.
type rcloneSizeResult struct {
	Count    int64 `json:"count"`
	Bytes    int64 `json:"bytes"`
	Sizeless int64 `json:"sizeless"`
}

type rcloneLogLine struct {
	Level string       `json:"level"`
	Msg   string       `json:"msg"`
	Stats *rcloneStats `json:"stats"`
}

type rcloneStats struct {
	Bytes          int64   `json:"bytes"`
	TotalBytes     int64   `json:"totalBytes"`
	Speed          float64 `json:"speed"`
	ETA            *int64  `json:"eta"`
	Transfers      int64   `json:"transfers"`
	TotalTransfers int64   `json:"totalTransfers"`
}

// ParseRcloneLine parses one JSON log line from rclone --use-json-log output.
// Returns (MediaProgressMsg, true) for stats lines, (RcloneErrorMsg, true) for
// error lines, and (nil, false) for all other lines (info, debug, etc.).
// Exported so it can be tested from the _test package.
func ParseRcloneLine(line []byte) (any, bool) {
	var entry rcloneLogLine
	if err := json.Unmarshal(line, &entry); err != nil {
		return nil, false
	}
	if entry.Stats != nil {
		return MediaProgressMsg{
			TransferredBytes: entry.Stats.Bytes,
			TotalBytes:       entry.Stats.TotalBytes,
			Speed:            entry.Stats.Speed,
			ETA:              entry.Stats.ETA,
			FilesDone:        entry.Stats.Transfers,
			FilesTotal:       entry.Stats.TotalTransfers,
		}, true
	}
	if entry.Level == "error" {
		return RcloneErrorMsg{Text: entry.Msg}, true
	}
	return nil, false
}

// Runner orchestrates database and media backup operations.
type Runner interface {
	RunMedia(remote, srcDir string) error
	RunDatabase(container, pgUser, destPath string) error
}

// BackupRunner is the production implementation of Runner.
type BackupRunner struct {
	exec       docker.Executor
	rcloneConf string // path to --config file for all rclone calls
}

// New returns a BackupRunner. rcloneConf must be the path to the isolated
// rclone config (constitution Principle V). Panics if empty.
func New(exec docker.Executor, rcloneConf string) Runner {
	if rcloneConf == "" {
		panic("backup.New: rcloneConf must not be empty (constitution Principle V)")
	}
	return &BackupRunner{exec: exec, rcloneConf: rcloneConf}
}

// RunDatabase dumps all databases from the Postgres container via pg_dumpall,
// gzips the output, and writes it to destPath.
func (r *BackupRunner) RunDatabase(container, pgUser, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("create dump dir: %w", err)
	}

	out, err := r.exec.Exec(container, "pg_dumpall", "-U", pgUser)
	if err != nil {
		return fmt.Errorf("pg_dumpall in %s: %w", container, err)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create dump file: %w", err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	if _, err := gz.Write(out); err != nil {
		_ = gz.Close()
		return fmt.Errorf("write gzip: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("flush gzip: %w", err)
	}
	return nil
}

// RunMedia syncs srcDir to the rclone remote using `rclone sync`.
// rcloneConf MUST be non-empty; New() panics if it is (constitution Principle V).
func (r *BackupRunner) RunMedia(remote, srcDir string) error {
	args := []string{"--config", r.rcloneConf, "sync", srcDir, remote}
	cmd := exec.Command("rclone", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rclone sync: %w", err)
	}
	return nil
}

// Run orchestrates a full backup: database dump → upload dump → media sync.
// Progress, errors, and completion are sent to ch for live TUI display.
func Run(
	rcloneConf, container, pgUser, uploadLocation, rcloneRemote string,
	executor docker.Executor,
	ch chan<- any,
) {
	send := func(msg any) {
		select {
		case ch <- msg:
		default:
		}
	}

	r := New(executor, rcloneConf)

	send(ProgressMsg{Text: "Starting database backup..."})
	dumpPath := filepath.Join(os.TempDir(),
		fmt.Sprintf("immich-db-%s.sql.gz", time.Now().Format("20060102-150405")))
	if err := r.RunDatabase(container, pgUser, dumpPath); err != nil {
		send(ErrorMsg{Err: fmt.Errorf("database backup: %w", err)})
		close(ch)
		return
	}

	send(ProgressMsg{Text: "Uploading database dump to remote..."})
	remoteDBDir := rcloneRemote + "/db"
	uploadOut, uploadErr := exec.Command("rclone", "--config", rcloneConf, "copy", dumpPath, remoteDBDir).CombinedOutput()
	_ = os.Remove(dumpPath) // best-effort; uploaded or failed, temp file is no longer needed
	if uploadErr != nil {
		send(ErrorMsg{Err: fmt.Errorf("upload database dump: %w: %s", uploadErr, strings.TrimSpace(string(uploadOut)))})
		close(ch)
		return
	}

	send(ProgressMsg{Text: "Database dump uploaded. Starting media sync..."})
	if err := r.RunMedia(rcloneRemote, uploadLocation); err != nil {
		send(ErrorMsg{Err: fmt.Errorf("media sync: %w", err)})
		close(ch)
		return
	}
	send(ProgressMsg{Text: "Media sync complete."})
	send(DoneMsg{})
	close(ch)
}
