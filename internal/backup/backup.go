// internal/backup/backup.go
package backup

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/daksh7011/immich-backup/internal/docker"
	"github.com/daksh7011/immich-backup/internal/rclonebin"
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

// PartialError reports a media sync where rclone skipped some files
// (--ignore-errors) and exited non-zero. The rest of the library was synced,
// but the run is incomplete and must not be recorded as a success.
type PartialError struct {
	FileErrors int   // error-level lines rclone logged
	Err        error // rclone's exit error
}

func (e *PartialError) Error() string {
	return fmt.Sprintf("incomplete: %d file error(s), %v", e.FileErrors, e.Err)
}

func (e *PartialError) Unwrap() error { return e.Err }

// rcloneBin returns the rclone executable invoked for uploads and syncs.
// It resolves fallback install dirs because scheduled runs get a minimal
// PATH. A variable so tests can substitute a fake binary.
var rcloneBin = rclonebin.Path

// dbRemoteDir is the subdirectory of the remote that holds database dumps.
// The media sync excludes it so `rclone sync` never deletes uploaded dumps.
const dbRemoteDir = "db"

// dbRemotePath returns the db/ directory inside remote: the directory the
// media sync's --exclude /db/** protects. Plain concatenation breaks for a
// remote with an empty path ("nas:" gives "nas:/db", an absolute path on
// sftp, ftp, smb and local) and for a trailing slash ("b2:x/" gives
// "b2:x//db").
func dbRemotePath(remote string) string {
	trimmed := strings.TrimRight(remote, "/")
	if strings.HasSuffix(trimmed, ":") && trimmed == remote {
		return remote + dbRemoteDir // "nas:" → "nas:db"
	}
	return trimmed + "/" + dbRemoteDir // "nas:/" → "nas:/db", "b2:x/" → "b2:x/db"
}

// rclone exit codes that report individual files failing while the rest of
// the run went on: 1 = uncategorised error, 4 = file not found,
// 5 = temporary error, 6 = less serious errors, 9 = no files transferred.
// Any other code (2 usage error, 3 directory not found, 7 fatal error, 8
// transfer limit) means the run as a whole failed.
// See https://rclone.org/docs/#exit-code
var rcloneFileErrorExitCodes = map[int]bool{1: true, 4: true, 5: true, 6: true, 9: true}

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
	RunDatabase(ctx context.Context, container, pgUser, destPath string) error
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

// RunDatabase dumps all databases from the Postgres container via pg_dumpall
// and streams the output through gzip into destPath, so the dump is never
// held in memory. The file is created 0600 because it holds password hashes
// and API keys, and it is removed again if the dump fails. Cancelling ctx
// aborts the dump.
func (r *BackupRunner) RunDatabase(ctx context.Context, container, pgUser, destPath string) (err error) {
	if err := os.MkdirAll(filepath.Dir(destPath), 0700); err != nil {
		return fmt.Errorf("create dump dir: %w", err)
	}

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create dump file: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close dump file: %w", cerr)
		}
		if err != nil {
			_ = os.Remove(destPath)
		}
	}()

	gz := gzip.NewWriter(f)
	if err := r.exec.ExecStream(ctx, gz, container, "pg_dumpall", "-U", pgUser); err != nil {
		_ = gz.Close()
		return fmt.Errorf("pg_dumpall in %s: %w", container, err)
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
		"--use-json-log", "--stats", "1s", "--log-level", "INFO",
		"--transfers", "1",
	}
	cmd := exec.CommandContext(ctx, rcloneBin(), args...)

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

// CheckUploadLocation verifies that dir exists, is a readable directory and
// contains at least one entry. rclone sync mirrors deletions, so syncing from
// an unmounted volume or an unreadable path (e.g. macOS privacy denial) would
// wipe the remote copy of the library — refuse instead.
func CheckUploadLocation(dir string) error {
	if dir == "" {
		return fmt.Errorf("upload_location is not set")
	}
	info, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("upload_location %s does not exist (is the volume mounted?)", dir)
	}
	if err != nil {
		return fmt.Errorf("upload_location %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("upload_location %s is not a directory", dir)
	}
	f, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("upload_location %s is not readable: %w", dir, err)
	}
	defer f.Close()
	names, err := f.Readdirnames(1)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("upload_location %s is not readable: %w", dir, err)
	}
	if len(names) == 0 {
		return fmt.Errorf("upload_location %s is empty (is the volume mounted?); refusing to sync and delete remote media", dir)
	}
	return nil
}

// mediaSyncArgs builds the rclone argv for the media sync. The remote's db/
// subdirectory is excluded: it holds the dumps uploaded by RunDBUpload, and
// without the exclude `rclone sync` would delete them as extraneous files.
// Excluded files are left untouched on the remote (no --delete-excluded).
// INFO is the lowest log level that still carries the stats and error lines
// ParseRcloneLine needs; DEBUG adds a line per unchanged file to rclone.log.
func mediaSyncArgs(rcloneConf, srcDir, remote string, opts MediaOpts) []string {
	return []string{
		"--config", rcloneConf,
		"sync", srcDir, remote,
		"--exclude", "/" + dbRemoteDir + "/**",
		"--use-json-log", "--stats", "1s", "--log-level", "INFO",
		"--ignore-errors",
		"--fast-list",
		"--transfers", strconv.Itoa(opts.Transfers),
		"--checkers", strconv.Itoa(opts.Checkers),
		"--buffer-size", opts.BufferSize,
	}
}

// mediaSyncResult maps rclone's exit status to RunMedia's error. Any non-zero
// exit is an error; it is a *PartialError only when rclone reported file-level
// errors and exited by itself with a file-level exit code. rclone killed by a
// signal (OOM killer, shutdown) never finished, so that is a plain error.
// A zero exit is success even if errors were logged: rclone logs failed
// attempts at error level and then succeeds on retry.
func mediaSyncResult(waitErr error, fileErrors int) error {
	if waitErr == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if fileErrors > 0 && errors.As(waitErr, &exitErr) && exitErr.Exited() &&
		rcloneFileErrorExitCodes[exitErr.ExitCode()] {
		return fmt.Errorf("rclone sync: %w", &PartialError{FileErrors: fileErrors, Err: waitErr})
	}
	return fmt.Errorf("rclone sync: %w", waitErr)
}

// RunMedia syncs srcDir to remote using rclone sync with JSON logging.
// Progress and file-level errors are sent to ch. ch may be nil (progress is silently discarded).
// If ctx is cancelled the rclone subprocess is killed immediately.
// A non-zero rclone exit always returns an error; see mediaSyncResult.
func (r *BackupRunner) RunMedia(ctx context.Context, remote, srcDir string, opts MediaOpts, ch chan<- any) error {
	if err := CheckUploadLocation(srcDir); err != nil {
		return err
	}

	// Sync with JSON log streaming.
	cmd := exec.CommandContext(ctx, rcloneBin(), mediaSyncArgs(r.rcloneConf, srcDir, remote, opts)...)

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

	return mediaSyncResult(cmd.Wait(), fileErrors)
}

// Run orchestrates a full backup: database dump → upload dump → media sync.
// Progress, errors, and completion are sent to ch for live TUI display.
// If ctx is cancelled in-flight, the active rclone subprocess or dump is
// stopped and the channel is closed without sending DoneMsg. The channel is
// closed only after the temp dump is removed, so a caller that exits once it
// closes leaves nothing behind.
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
	defer close(ch)

	// send is a blocking send used for phase transitions and terminal messages
	// (PhaseMsg, ErrorMsg, DoneMsg). These must not be dropped — losing a terminal
	// message leaves the TUI frozen. Progress-tick messages (DBUploadProgressMsg,
	// MediaProgressMsg) use the non-blocking sendMsg helper in their respective
	// Run* methods and are safe to drop under backpressure.
	send := func(msg any) { ch <- msg }

	// fail reports err as the run's error unless ctx was cancelled, in which
	// case the channel just closes.
	fail := func(err error) {
		if ctx.Err() == nil {
			send(ErrorMsg{Err: err})
		}
	}

	if logWriter == nil {
		logWriter = io.Discard
	}
	_, _ = fmt.Fprintf(logWriter, "\n--- backup run %s ---\n", time.Now().UTC().Format(time.RFC3339))

	r := New(executor, rcloneConf, logWriter)

	if !skipDB {
		send(PhaseMsg{Phase: PhaseDBDump})
		// A private (0700) dir keeps the dump unreadable to other users of a
		// shared /tmp while keeping its timestamped name for the remote.
		dumpDir, err := os.MkdirTemp("", "immich-backup-")
		if err != nil {
			fail(fmt.Errorf("database backup: create temp dir: %w", err))
			return
		}
		// Removed on every path, including a cancelled run, before ch closes.
		defer os.RemoveAll(dumpDir)
		dumpPath := filepath.Join(dumpDir,
			fmt.Sprintf("immich-db-%s.sql.gz", time.Now().Format("20060102-150405")))
		if err := r.RunDatabase(ctx, container, pgUser, dumpPath); err != nil {
			fail(fmt.Errorf("database backup: %w", err))
			return
		}

		send(PhaseMsg{Phase: PhaseDBUpload})
		uploadErr := r.RunDBUpload(ctx, dumpPath, dbRemotePath(rcloneRemote), ch)
		_ = os.Remove(dumpPath) // free the space before the media sync
		if uploadErr != nil {
			fail(fmt.Errorf("upload database dump: %w", uploadErr))
			return
		}
	}

	if !skipMedia {
		send(PhaseMsg{Phase: PhaseMedia})
		if err := r.RunMedia(ctx, rcloneRemote, uploadLocation, opts, ch); err != nil {
			fail(fmt.Errorf("media sync: %w", err))
			return
		}
	}

	send(DoneMsg{})
}
