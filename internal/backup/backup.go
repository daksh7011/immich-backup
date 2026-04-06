// internal/backup/backup.go
package backup

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/daksh7011/immich-backup/internal/docker"
)

// Progress message types sent to the TUI channel during a backup run.
// These are defined here so internal/tui/backup_model.go can import them
// without creating an import cycle.
type ProgressMsg struct{ Text string }
type ErrorMsg struct{ Err error }
type DoneMsg struct{}

// Runner orchestrates database and media backup operations.
type Runner interface {
	RunMedia(remote, srcDir string) error
	RunDatabase(container, pgUser, pgDB, destPath string) error
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
func (r *BackupRunner) RunDatabase(container, pgUser, pgDB, destPath string) error {
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
	defer gz.Close()

	if _, err := io.WriteString(gz, string(out)); err != nil {
		return fmt.Errorf("write gzip: %w", err)
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

// Run orchestrates a full backup: database then media.
// Progress, errors, and completion are sent to ch for live TUI display.
func Run(
	rcloneConf, container, pgUser, pgDB, uploadLocation, rcloneRemote string,
	exec docker.Executor,
	ch chan<- any,
) {
	send := func(msg any) {
		select {
		case ch <- msg:
		default:
		}
	}

	r := New(exec, rcloneConf)

	send(ProgressMsg{Text: "Starting database backup..."})
	dumpPath := filepath.Join(os.TempDir(),
		fmt.Sprintf("immich-db-%s.sql.gz", time.Now().Format("20060102-150405")))
	if err := r.RunDatabase(container, pgUser, pgDB, dumpPath); err != nil {
		send(ErrorMsg{Err: fmt.Errorf("database backup: %w", err)})
		close(ch)
		return
	}
	send(ProgressMsg{Text: "Database backup complete. Starting media sync..."})

	if err := r.RunMedia(rcloneRemote, uploadLocation); err != nil {
		send(ErrorMsg{Err: fmt.Errorf("media sync: %w", err)})
		close(ch)
		return
	}
	send(ProgressMsg{Text: "Media sync complete."})
	send(DoneMsg{})
	close(ch)
}
