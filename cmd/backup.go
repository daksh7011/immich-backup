// cmd/backup.go
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/daksh7011/immich-backup/internal/backup"
	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/daemon"
	"github.com/daksh7011/immich-backup/internal/docker"
	"github.com/daksh7011/immich-backup/internal/doctor"
	"github.com/daksh7011/immich-backup/internal/rcloneconf"
	"github.com/daksh7011/immich-backup/internal/status"
	"github.com/daksh7011/immich-backup/internal/tui"
	"github.com/spf13/cobra"
)

func newBackupCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "backup",
		Short: "Run a backup now",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBackup(cmd, GetConfig(cmd), defaultBackupDeps())
		},
	}
	c.Flags().Bool("skip-db", false, "Skip database dump and upload")
	c.Flags().Bool("skip-media", false, "Skip media sync")
	c.Flags().Bool("remote", false, "Interactively select backup remote and save path for future pre-fill")
	return c
}

// backupClient is the Docker client a backup run needs: an Executor that is
// closed when the run ends.
type backupClient interface {
	docker.Executor
	Close()
}

// backupDeps holds the backup run's external dependencies so tests can drive
// the prerequisite and status paths without Docker or rclone.
type backupDeps struct {
	newClient      func() (backupClient, error)
	check          func(docker.Executor, *config.Config, string) []doctor.CheckResult
	statusPath     string
	rcloneConfPath string
	// prereqWait bounds how long a headless run waits for Docker and
	// Postgres to come up; zero fails at once. prereqPoll is the interval
	// between checks.
	prereqWait time.Duration
	prereqPoll time.Duration
}

// A scheduled run that catches up a missed backup at boot can start before
// dockerd has restarted the Immich containers, since a user unit cannot order
// itself after the system's docker.service. It waits this long for them.
const (
	defaultPrereqWait = 10 * time.Minute
	defaultPrereqPoll = 30 * time.Second
)

// runStopTimeout bounds the wait, after the display returns, for an
// interrupted run to kill rclone and remove its temp dump.
const runStopTimeout = 10 * time.Second

func defaultBackupDeps() backupDeps {
	return backupDeps{
		newClient: func() (backupClient, error) {
			c, err := docker.NewClient()
			if err != nil {
				return nil, err // avoid a non-nil interface holding a nil *Client
			}
			return c, nil
		},
		check:          doctor.Check,
		statusPath:     config.StatusFilePath(),
		rcloneConfPath: config.RcloneConfigPath(),
		prereqWait:     defaultPrereqWait,
		prereqPoll:     defaultPrereqPoll,
	}
}

// errBackupCancelled means the user backed out of the --remote picker before
// anything ran. It is not recorded as a backup attempt.
var errBackupCancelled = errors.New("backup cancelled")

// runBackup validates the flags, runs one backup attempt and records its
// outcome in the status file — including prerequisite failures, so a scheduled
// run that fails early is visible in `immich-backup status`. Any failure is
// returned so the process exits non-zero.
func runBackup(cmd *cobra.Command, cfg *config.Config, deps backupDeps) error {
	skipDB, _ := cmd.Flags().GetBool("skip-db")
	skipMedia, _ := cmd.Flags().GetBool("skip-media")
	pickRemote, _ := cmd.Flags().GetBool("remote")

	// Flag misuse is not a backup attempt: report it without touching status.
	if skipDB && skipMedia {
		return fmt.Errorf("--skip-db and --skip-media together would back up nothing")
	}
	if pickRemote && !isTTY() {
		return fmt.Errorf("--remote requires an interactive terminal")
	}

	start := time.Now().UTC()
	// The outcome is recorded once: as soon as the run reports it (before a
	// finished TUI is dismissed), or else from the error returned below.
	var once sync.Once
	record := func(err error) { once.Do(func() { recordRun(deps.statusPath, start, err) }) }
	err := runBackupAttempt(cfg, deps, skipDB, skipMedia, pickRemote, record)
	if errors.Is(err, errBackupCancelled) {
		fmt.Println("Backup cancelled.")
		return nil
	}
	record(err)
	return err
}

// runBackupAttempt checks prerequisites and runs the backup, returning nil only
// when everything succeeded. Prerequisite failures are logged with a remedy
// before they are returned. record is called with the run's outcome the
// moment backup.Run reports it.
func runBackupAttempt(cfg *config.Config, deps backupDeps, skipDB, skipMedia, pickRemote bool, record func(error)) error {
	// Create the daemon log before anything can fail, so a manual run
	// repairs a missing log dir that would stop scheduled runs from starting.
	if err := daemon.EnsureLogFile(cfg.Daemon.LogPath); err != nil {
		slog.Error("cannot create daemon log", "error", err,
			"remedy", "check daemon.log_path in ~/.immich-backup/config.yaml and its directory permissions")
		return fmt.Errorf("prepare daemon log: %w", err)
	}

	// ctx is cancelled by SIGTERM (launchd unloading the job, shutdown),
	// SIGHUP (terminal closed), SIGINT, or Ctrl+C in the TUI. Cancelling
	// kills rclone and lets the run record why it stopped.
	ctx, cancel := signalContext()
	defer cancel()

	// Prerequisite checks — fail fast
	client, err := deps.newClient()
	if err != nil {
		slog.Error("docker socket unreachable", "error", err,
			"remedy", "ensure Docker is running")
		return fmt.Errorf("docker client: %w", err)
	}
	defer client.Close()

	interactive := isTTY()
	results := deps.check(client, cfg, deps.rcloneConfPath)
	if doctor.AnyFailed(results) && !interactive {
		results = waitForPrerequisites(ctx, deps, client, cfg, results)
	}
	// The checks do not watch ctx, so a signal that arrived meanwhile only
	// shows up here.
	if err := cancelledBeforeRun(ctx, interactive, results); err != nil {
		return err
	}
	if doctor.AnyFailed(results) {
		for _, r := range results {
			if !r.OK {
				slog.Error("prerequisite check failed",
					"check", r.Name, "message", r.Message, "remedy", r.Remedy)
			}
		}
		return prerequisiteError(results)
	}

	// Open log file before registering cancel so the defer order (LIFO) is:
	//   1. cancel()        — signals goroutine to stop writing
	//   2. logFile.Close() — safe to close after goroutine is signalled
	logFile := openRcloneLog(config.RcloneLogPath())
	defer logFile.Close()

	// Resolve effective remote: --remote triggers interactive picker, saves the chosen
	// path to remote_paths for future pre-fill but does not update rclone_remote itself.
	effectiveRemote := cfg.Backup.RcloneRemote
	if pickRemote {
		remotes, err := rcloneconf.ListRemotes(deps.rcloneConfPath)
		if err != nil || len(remotes) == 0 {
			return fmt.Errorf("no rclone remotes found in %s — run `configure` first", deps.rcloneConfPath)
		}
		// Pre-fill path from saved remote_paths for the currently configured default remote.
		defaultName, _ := splitRemote(cfg.Backup.RcloneRemote)
		storedPath := cfg.Backup.RemotePaths[defaultName]
		picker := tui.NewRemotePickerModel(remotes, defaultName+":"+storedPath)
		p := tea.NewProgram(picker, tea.WithoutSignalHandler())
		stop := context.AfterFunc(ctx, p.Quit)
		result, err := p.Run()
		stop()
		if ctx.Err() != nil {
			return errBackupCancelled
		}
		if err != nil {
			return fmt.Errorf("remote picker: %w", err)
		}
		final := result.(tui.RemotePickerModel)
		if !final.Done() || final.Aborted() {
			return errBackupCancelled
		}
		effectiveRemote = final.Result()

		// Persist the chosen path so future runs pre-fill it.
		remoteName, remotePath := splitRemote(effectiveRemote)
		if cfg.Backup.RemotePaths == nil {
			cfg.Backup.RemotePaths = make(map[string]string)
		}
		cfg.Backup.RemotePaths[remoteName] = remotePath
		if err := config.Save(config.DefaultConfigPath(), cfg); err != nil {
			slog.Warn("could not persist remote path selection", "error", err)
		}
	}

	src := make(chan any, 16)
	go backup.Run(
		ctx,
		deps.rcloneConfPath,
		cfg.Immich.PostgresContainer,
		cfg.Immich.PostgresUser,
		cfg.Immich.UploadLocation,
		effectiveRemote,
		client,
		skipDB, skipMedia,
		backup.MediaOpts{
			Transfers:  cfg.Backup.Transfers,
			Checkers:   cfg.Backup.Checkers,
			BufferSize: cfg.Backup.BufferSize,
		},
		logFile,
		src,
	)
	w := watchRun(src, record)

	var displayErr error
	if isTTY() {
		displayErr = runBackupTUI(ctx, cancel, w.out, skipDB, skipMedia)
	} else {
		displayErr = runBackupHeadless(ctx, w.out)
	}
	return finishRun(ctx, cancel, w, displayErr)
}

// finishRun stops a run the display has stopped following and returns its
// result. The run's own outcome wins when it reported one; otherwise it was
// interrupted (signal or Ctrl+C), or the display failed. It waits for the run
// to wind down so rclone is killed and the temp dump removed before the
// process exits.
func finishRun(ctx context.Context, cancel context.CancelFunc, w *runWatcher, displayErr error) error {
	interrupted := context.Cause(ctx) // read before cancel() sets it
	cancel()
	w.stop(runStopTimeout)
	if ended, err := w.outcome(); ended {
		return err
	}
	if interrupted != nil {
		return fmt.Errorf("backup aborted: %w", interrupted)
	}
	if displayErr != nil {
		return displayErr
	}
	return fmt.Errorf("backup stopped before it finished")
}

// runBackupTUI shows the live progress TUI until the user dismisses it. A
// signal (e.g. SIGHUP when the terminal closes) quits the TUI too.
// signalContext is the only signal handler: Bubble Tea's own would race the
// AfterFunc to send a quit message and, on losing, block forever on a send
// nothing reads, hanging p.Run.
func runBackupTUI(ctx context.Context, cancel context.CancelFunc, ch <-chan any, skipDB, skipMedia bool) error {
	p := tea.NewProgram(tui.NewBackupModel(ch, cancel, skipDB, skipMedia), tea.WithoutSignalHandler())
	stop := context.AfterFunc(ctx, p.Quit)
	defer stop()
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("backup TUI: %w", err)
	}
	return nil
}

// signalContext returns a context cancelled by SIGINT, SIGTERM or SIGHUP,
// with the signal as its cause ("signal: terminated"). Catching them keeps
// the process alive long enough to stop rclone, remove the temp dump and
// record the run, which Go's default handling (exit at once) would skip.
func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(context.Background())
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		select {
		case s := <-sigs:
			cancel(fmt.Errorf("signal: %v", s))
		case <-ctx.Done():
		}
	}()
	return ctx, func() {
		signal.Stop(sigs)
		cancel(context.Canceled)
	}
}

// cancelledBeforeRun returns the error for a signal that arrived during the
// prerequisite checks, or nil if ctx is still live. At a terminal it is the
// user's Ctrl+C before anything ran, so it is not recorded. A headless run
// whose checks failed gets nil, so those failures are recorded as when the
// boot wait runs out; one whose checks passed is recorded as aborted, as a
// run stopped mid-way would be.
func cancelledBeforeRun(ctx context.Context, interactive bool, results []doctor.CheckResult) error {
	if ctx.Err() == nil {
		return nil
	}
	if interactive {
		return errBackupCancelled
	}
	if doctor.AnyFailed(results) {
		return nil
	}
	return fmt.Errorf("backup aborted: %w", context.Cause(ctx))
}

// runWatcher relays backup.Run's messages to the display and records the
// run's outcome the moment DoneMsg or ErrorMsg arrives. A finished TUI waits
// for a key, so recording after it exits would lose the result if the
// terminal went away first.
type runWatcher struct {
	out        chan any      // messages for the display; closed after the run
	finished   chan struct{} // closed once backup.Run has closed its channel
	detach     chan struct{} // closed when the display stops reading out
	detachOnce sync.Once

	mu    sync.Mutex
	ended bool
	err   error
}

func watchRun(src <-chan any, record func(error)) *runWatcher {
	w := &runWatcher{out: make(chan any, 16), finished: make(chan struct{}), detach: make(chan struct{})}
	go func() {
		defer close(w.finished)
		defer close(w.out)
		for msg := range src {
			switch v := msg.(type) {
			case backup.DoneMsg:
				w.end(nil, record)
			case backup.ErrorMsg:
				w.end(v.Err, record)
			}
			select {
			case w.out <- msg:
			case <-w.detach:
			}
		}
	}()
	return w
}

func (w *runWatcher) end(err error, record func(error)) {
	w.mu.Lock()
	w.ended, w.err = true, err
	w.mu.Unlock()
	record(err)
}

// outcome reports whether the run sent a terminal message, and its error.
func (w *runWatcher) outcome() (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ended, w.err
}

// stop detaches the display and waits up to timeout for backup.Run to close
// its channel.
func (w *runWatcher) stop(timeout time.Duration) {
	w.detachOnce.Do(func() { close(w.detach) })
	select {
	case <-w.finished:
	case <-time.After(timeout):
		slog.Warn("backup did not stop in time; temp files may be left behind", "timeout", timeout)
	}
}

// waitForPrerequisites re-runs the checks while the only failures are ones
// that clear up on their own after boot (Docker not up yet, Postgres
// container not started or Postgres not yet accepting connections), for at
// most deps.prereqWait. Any other failure, a permission error, or a
// cancelled ctx returns the results at once.
func waitForPrerequisites(ctx context.Context, deps backupDeps, ex docker.Executor, cfg *config.Config, results []doctor.CheckResult) []doctor.CheckResult {
	if deps.prereqWait <= 0 || !onlyTransientFailures(results) {
		return results
	}
	slog.Warn("waiting for Docker and Postgres", "max_wait", deps.prereqWait,
		"reason", prerequisiteError(results).Error())
	deadline := time.Now().Add(deps.prereqWait)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return results
		case <-time.After(deps.prereqPoll):
		}
		results = deps.check(ex, cfg, deps.rcloneConfPath)
		if !doctor.AnyFailed(results) {
			slog.Info("prerequisites ready")
			return results
		}
		if !onlyTransientFailures(results) {
			return results
		}
	}
	return results
}

// transientChecks are the checks that fail while the machine is still
// booting and pass on their own once Docker has started the containers and
// Postgres accepts connections.
var transientChecks = map[string]bool{"Docker Socket": true, "Postgres Container": true}

// onlyTransientFailures reports whether every failed check is a transient
// one. A permission error on the socket never clears up by waiting.
func onlyTransientFailures(results []doctor.CheckResult) bool {
	for _, r := range results {
		if r.OK {
			continue
		}
		if !transientChecks[r.Name] || strings.Contains(strings.ToLower(r.Message), "permission denied") {
			return false
		}
	}
	return true
}

// prerequisiteError joins the failed checks into one "<check>: <message>" list,
// which is both the returned error and the recorded status.
func prerequisiteError(results []doctor.CheckResult) error {
	var failed []string
	for _, r := range results {
		if !r.OK {
			failed = append(failed, r.Name+": "+r.Message)
		}
	}
	return fmt.Errorf("prerequisite checks failed: %s", strings.Join(failed, "; "))
}

// recordRun saves the outcome of the attempt that started at start. A status
// write failure is logged but never replaces the backup's own result.
func recordRun(path string, start time.Time, err error) {
	run := &status.LastRun{Time: start}
	recordOutcome(run, err)
	if saveErr := status.Record(path, run); saveErr != nil {
		slog.Warn("could not record backup status", "path", path, "error", saveErr)
	}
}

// recordOutcome sets run's Result and Error from the backup's final error.
// A media sync where rclone skipped files is "partial"; any other error is
// "error". Only a nil error counts as success.
func recordOutcome(run *status.LastRun, err error) {
	var partial *backup.PartialError
	switch {
	case err == nil:
		run.Result = status.ResultSuccess
		run.Error = ""
		return
	case errors.As(err, &partial):
		run.Result = status.ResultPartial
	default:
		run.Result = status.ResultError
	}
	run.Error = err.Error()
}

// maxRcloneLogSize is the size at which rclone.log is rotated to
// rclone.log.1 at the start of a run, so daily runs cannot fill the disk.
const maxRcloneLogSize = 50 << 20

// openRcloneLog opens the rclone log file in append mode, creating it (and
// parent dirs) if necessary. A log over maxRcloneLogSize first replaces
// <path>.1, so at most two files are kept. On any error it returns io.Discard
// so the caller always gets a valid writer.
func openRcloneLog(path string) io.WriteCloser {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nopCloser{io.Discard}
	}
	if info, err := os.Stat(path); err == nil && info.Size() > maxRcloneLogSize {
		if err := os.Rename(path, path+".1"); err != nil {
			slog.Warn("could not rotate rclone log", "path", path, "error", err)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nopCloser{io.Discard}
	}
	return f
}

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

// isTTY reports whether both stdin and stdout are connected to a terminal.
// Bubble Tea requires both for interactive rendering and keypress handling.
// When false (e.g. systemd, cron, piped output) the backup runs headless.
func isTTY() bool {
	for _, f := range []*os.File{os.Stdin, os.Stdout} {
		fi, err := f.Stat()
		if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
			return false
		}
	}
	return true
}

// splitRemote splits "name:path" into its two components.
// For "b2:immich-backup" it returns ("b2", "immich-backup").
// If there is no colon the whole string is the name and path is empty.
func splitRemote(remote string) (name, path string) {
	if i := strings.IndexByte(remote, ':'); i >= 0 {
		return remote[:i], remote[i+1:]
	}
	return remote, ""
}

// runBackupHeadless drains the backup event channel and logs each event via
// slog. Used when there is no terminal (e.g. systemd service).
func runBackupHeadless(ctx context.Context, ch <-chan any) error {
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				// channel closed without DoneMsg → cancelled
				if err := ctx.Err(); err != nil {
					return err
				}
				return fmt.Errorf("backup channel closed unexpectedly")
			}
			switch v := msg.(type) {
			case backup.PhaseMsg:
				switch v.Phase {
				case backup.PhaseDBDump:
					slog.Info("backup: dumping database")
				case backup.PhaseDBUpload:
					slog.Info("backup: uploading database dump")
				case backup.PhaseMedia:
					slog.Info("backup: syncing media")
				}
			case backup.RcloneErrorMsg:
				slog.Warn("backup: rclone error", "error", v.Text)
			case backup.DoneMsg:
				slog.Info("backup: complete")
				return nil
			case backup.ErrorMsg:
				return v.Err
			}
		case <-ctx.Done():
			// A signal cancelled the run; finishRun waits for it to stop.
			return ctx.Err()
		}
	}
}
