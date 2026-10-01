// cmd/backup.go
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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
}

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
	err := runBackupAttempt(cfg, deps, skipDB, skipMedia, pickRemote)
	if errors.Is(err, errBackupCancelled) {
		fmt.Println("Backup cancelled.")
		return nil
	}
	recordRun(deps.statusPath, start, err)
	return err
}

// runBackupAttempt checks prerequisites and runs the backup, returning nil only
// when everything succeeded. Prerequisite failures are logged with a remedy
// before they are returned.
func runBackupAttempt(cfg *config.Config, deps backupDeps, skipDB, skipMedia, pickRemote bool) error {
	// Create the daemon log before anything can fail, so a manual run
	// repairs a missing log dir that would stop scheduled runs from starting.
	if err := daemon.EnsureLogFile(cfg.Daemon.LogPath); err != nil {
		slog.Error("cannot create daemon log", "error", err,
			"remedy", "check daemon.log_path in ~/.immich-backup/config.yaml and its directory permissions")
		return fmt.Errorf("prepare daemon log: %w", err)
	}

	// Prerequisite checks — fail fast
	client, err := deps.newClient()
	if err != nil {
		slog.Error("docker socket unreachable", "error", err,
			"remedy", "ensure Docker is running")
		return fmt.Errorf("docker client: %w", err)
	}
	defer client.Close()

	results := deps.check(client, cfg, deps.rcloneConfPath)
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

	// ctx is cancelled when the user presses Ctrl+C, which kills rclone.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
		p := tea.NewProgram(picker)
		result, err := p.Run()
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

	ch := make(chan any, 16)
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
		ch,
	)

	if !isTTY() {
		return runBackupHeadless(ctx, ch)
	}
	model := tui.NewBackupModel(ch, cancel, skipDB, skipMedia)
	p := tea.NewProgram(model)
	result, err := p.Run()
	if err != nil {
		// The TUI itself failed; the deferred cancel() stops the backup, so it
		// did not complete and is recorded as an error.
		return fmt.Errorf("backup TUI: %w", err)
	}
	final := result.(tui.BackupModel)
	if err := final.Err(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		// Ctrl+C: the model cancelled ctx and quit without an error.
		return fmt.Errorf("backup aborted: %w", ctx.Err())
	}
	return nil
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

// openRcloneLog opens the rclone log file in append mode, creating it (and
// parent dirs) if necessary. On any error it returns io.Discard so the caller
// always gets a valid writer.
func openRcloneLog(path string) io.WriteCloser {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nopCloser{io.Discard}
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
			// Reached if an external caller cancels the context (e.g. a future
			// signal handler). Currently unreachable: defer cancel() fires only
			// after this function returns.
			return ctx.Err()
		}
	}
}
