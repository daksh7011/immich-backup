// internal/backup/backup.go
package backup

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/daksh7011/immich-backup/internal/docker"
)

// Message types sent to the TUI channel during a backup run.
// Defined here so internal/tui/backup_model.go can import them without a cycle.
type ErrorMsg struct{ Err error }
type DoneMsg struct{}

// BackupPhase identifies which phase of the backup pipeline is active.
type BackupPhase int

const (
	PhaseDBDump   BackupPhase = iota // pg_dumpall is running
	PhaseDBUpload                    // rclone copy of the DB dump is running
	PhaseMedia                       // rclone sync is running
)

// PhaseMsg is sent when the backup pipeline transitions to a new phase.
type PhaseMsg struct{ Phase BackupPhase }

// RcloneTransfer represents a single in-flight file transfer as reported by rclone.
type RcloneTransfer struct {
	Name       string  `json:"name"`
	Size       int64   `json:"size"`
	Bytes      int64   `json:"bytes"`
	Speed      float64 `json:"speed"`
	ETA        *int64  `json:"eta"`
	Percentage int64   `json:"percentage"`
}

// MediaOpts configures rclone performance parameters for a media sync run.
type MediaOpts struct {
	Transfers  int
	Checkers   int
	BufferSize string
}

// MediaProgressMsg is sent each stats tick during rclone sync.
type MediaProgressMsg struct {
	TransferredBytes int64
	TotalBytes       int64
	Speed            float64 // bytes/sec; 0 on first tick
	ETA              *int64  // nil = not yet known (rclone emits null on first tick)
	FilesDone        int64
	FilesTotal       int64
	Checks           int64
	TotalChecks      int64
	ElapsedTime      float64
	Transferring     []RcloneTransfer
}

// DBUploadProgressMsg is sent each stats tick while uploading the database dump.
type DBUploadProgressMsg struct {
	TransferredBytes int64
	TotalBytes       int64
	Speed            float64
	ETA              *int64
}

// RcloneErrorMsg is sent when rclone reports a file-level error.
// Non-fatal: backup continues because RunMedia uses --ignore-errors.
type RcloneErrorMsg struct {
	Text string
}

// Private JSON structs used only inside this package.
type rcloneLogLine struct {
	Level string       `json:"level"`
	Msg   string       `json:"msg"`
	Stats *rcloneStats `json:"stats"`
}

type rcloneStats struct {
	Bytes          int64            `json:"bytes"`
	TotalBytes     int64            `json:"totalBytes"`
	Speed          float64          `json:"speed"`
	ETA            *int64           `json:"eta"`
	Transfers      int64            `json:"transfers"`
	TotalTransfers int64            `json:"totalTransfers"`
	Checks         int64            `json:"checks"`
	TotalChecks    int64            `json:"totalChecks"`
	ElapsedTime    float64          `json:"elapsedTime"`
	Transferring   []RcloneTransfer `json:"transferring"`
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
			Checks:           entry.Stats.Checks,
			TotalChecks:      entry.Stats.TotalChecks,
			ElapsedTime:      entry.Stats.ElapsedTime,
			Transferring:     entry.Stats.Transferring,
		}, true
	}
	if entry.Level == "error" {
		return RcloneErrorMsg{Text: entry.Msg}, true
	}
	return nil, false
}

// sendMsg sends msg to ch without blocking. Safe to call with a nil ch.
func sendMsg(ch chan<- any, msg any) {
	if ch == nil {
		return
	}
	select {
	case ch <- msg:
	default:
	}
}

// Runner orchestrates database and media backup operations.
type Runner interface {
	RunDatabase(container, pgUser, destPath string) error
	RunDBUpload(ctx context.Context, dumpPath, remoteDir string, ch chan<- any) error
	RunMedia(ctx context.Context, remote, srcDir string, opts MediaOpts, ch chan<- any) error
}

// BackupRunner is the production implementation of Runner.
type BackupRunner struct {
	exec       docker.Executor
	rcloneConf string    // path to --config file for all rclone calls
	logWriter  io.Writer // receives raw rclone stderr lines; io.Discard if nil
}

// New returns a BackupRunner. rcloneConf must be the path to the isolated
// rclone config (constitution Principle V). Panics if empty.
// logWriter receives every raw rclone log line for persistent storage; pass
// nil to discard (e.g. in tests).
func New(exec docker.Executor, rcloneConf string, logWriter io.Writer) Runner {
	if rcloneConf == "" {
		panic("backup.New: rcloneConf must not be empty (constitution Principle V)")
	}
	if logWriter == nil {
		logWriter = io.Discard
	}
	return &BackupRunner{exec: exec, rcloneConf: rcloneConf, logWriter: logWriter}
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

// RunDBUpload copies dumpPath to remoteDir using rclone copy with JSON log
// streaming. DBUploadProgressMsg ticks are sent to ch on each stats line.
// If ctx is cancelled the rclone subprocess is killed immediately.
func (r *BackupRunner) RunDBUpload(ctx context.Context, dumpPath, remoteDir string, ch chan<- any) error {
	info, err := os.Stat(dumpPath)
	if err != nil {
		return fmt.Errorf("stat dump file: %w", err)
	}
	totalBytes := info.Size()
	// Send an initial tick so the TUI can show the bar at 0% immediately.
	sendMsg(ch, DBUploadProgressMsg{TotalBytes: totalBytes})

	args := []string{
		"--config", r.rcloneConf,
		"copy", dumpPath, remoteDir,
		"--use-json-log", "--stats", "1s", "--log-level", "DEBUG",
		"--transfers", "1",
	}
	cmd := exec.CommandContext(ctx, "rclone", args...)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("rclone stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("rclone copy start: %w", err)
	}

	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		_, _ = r.logWriter.Write(append(line, '\n'))

		var entry rcloneLogLine
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.Level == "error" {
			sendMsg(ch, RcloneErrorMsg{Text: entry.Msg})
			continue
		}
		if entry.Stats == nil {
			continue
		}
		tb := entry.Stats.TotalBytes
		if tb == 0 {
			tb = totalBytes // use file size when rclone hasn't computed it yet
		}
		sendMsg(ch, DBUploadProgressMsg{
			TransferredBytes: entry.Stats.Bytes,
			TotalBytes:       tb,
			Speed:            entry.Stats.Speed,
			ETA:              entry.Stats.ETA,
		})
	}

	if err := scanner.Err(); err != nil {
		_, _ = io.Copy(io.Discard, stderr)
		_ = cmd.Wait()
		return fmt.Errorf("rclone stderr read: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("rclone copy: %w", err)
	}
	return nil
}

// RunMedia syncs srcDir to remote using rclone sync with JSON logging.
// Progress and file-level errors are sent to ch. ch may be nil (progress is silently discarded).
// If ctx is cancelled the rclone subprocess is killed immediately.
func (r *BackupRunner) RunMedia(ctx context.Context, remote, srcDir string, opts MediaOpts, ch chan<- any) error {
	// Sync with JSON log streaming.
	args := []string{
		"--config", r.rcloneConf,
		"sync", srcDir, remote,
		"--use-json-log", "--stats", "1s", "--log-level", "DEBUG",
		"--ignore-errors",
		"--fast-list",
		"--transfers", strconv.Itoa(opts.Transfers),
		"--checkers", strconv.Itoa(opts.Checkers),
		"--buffer-size", opts.BufferSize,
	}
	cmd := exec.CommandContext(ctx, "rclone", args...)

	// cmd.Stdout is intentionally not set: rclone writes nothing meaningful to
	// stdout when --use-json-log is active, so we let it go to /dev/null.
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("rclone stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("rclone sync start: %w", err)
	}

	var fileErrors int
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		_, _ = r.logWriter.Write(append(line, '\n'))

		msg, ok := ParseRcloneLine(line)
		if !ok {
			continue
		}
		if _, isErr := msg.(RcloneErrorMsg); isErr {
			fileErrors++
		}
		sendMsg(ch, msg)
	}

	if err := scanner.Err(); err != nil {
		_, _ = io.Copy(io.Discard, stderr)
		_ = cmd.Wait()
		return fmt.Errorf("rclone stderr read: %w", err)
	}

	if err := cmd.Wait(); err != nil && fileErrors == 0 {
		// Non-zero exit with no captured RcloneErrorMsg values means a fatal rclone
		// error (e.g. auth failure, missing remote). Those errors appear as critical-
		// level or plain-text stderr before JSON logging is active, so fileErrors
		// stays 0 and we surface the exit error. If fileErrors > 0, rclone exited 1
		// due to --ignore-errors skipping files — that's expected partial success.
		return fmt.Errorf("rclone sync: %w", err)
	}
	return nil
}

// Run orchestrates a full backup: database dump → upload dump → media sync.
// Progress, errors, and completion are sent to ch for live TUI display.
// If ctx is cancelled in-flight, the active rclone subprocess is killed and
// the channel is closed without sending DoneMsg.
// skipDB skips the database dump+upload; skipMedia skips the rclone media sync.
func Run(
	ctx context.Context,
	rcloneConf, container, pgUser, uploadLocation, rcloneRemote string,
	executor docker.Executor,
	skipDB, skipMedia bool,
	opts MediaOpts,
	logWriter io.Writer,
	ch chan<- any,
) {
	// send is a blocking send used for phase transitions and terminal messages
	// (PhaseMsg, ErrorMsg, DoneMsg). These must not be dropped — losing a terminal
	// message leaves the TUI frozen. Progress-tick messages (DBUploadProgressMsg,
	// MediaProgressMsg) use the non-blocking sendMsg helper in their respective
	// Run* methods and are safe to drop under backpressure.
	send := func(msg any) { ch <- msg }

	if logWriter == nil {
		logWriter = io.Discard
	}
	_, _ = fmt.Fprintf(logWriter, "\n--- backup run %s ---\n", time.Now().UTC().Format(time.RFC3339))

	r := New(executor, rcloneConf, logWriter)

	if !skipDB {
		send(PhaseMsg{Phase: PhaseDBDump})
		dumpPath := filepath.Join(os.TempDir(),
			fmt.Sprintf("immich-db-%s.sql.gz", time.Now().Format("20060102-150405")))
		if err := r.RunDatabase(container, pgUser, dumpPath); err != nil {
			send(ErrorMsg{Err: fmt.Errorf("database backup: %w", err)})
			close(ch)
			return
		}

		send(PhaseMsg{Phase: PhaseDBUpload})
		remoteDBDir := rcloneRemote + "/db"
		uploadErr := r.RunDBUpload(ctx, dumpPath, remoteDBDir, ch)
		_ = os.Remove(dumpPath) // best-effort; uploaded or failed, temp file is no longer needed
		if uploadErr != nil {
			if ctx.Err() != nil {
				close(ch)
				return
			}
			send(ErrorMsg{Err: fmt.Errorf("upload database dump: %w", uploadErr)})
			close(ch)
			return
		}
	}

	if !skipMedia {
		send(PhaseMsg{Phase: PhaseMedia})
		if err := r.RunMedia(ctx, rcloneRemote, uploadLocation, opts, ch); err != nil {
			if ctx.Err() != nil {
				close(ch)
				return
			}
			send(ErrorMsg{Err: fmt.Errorf("media sync: %w", err)})
			close(ch)
			return
		}
	}

	send(DoneMsg{})
	close(ch)
}
