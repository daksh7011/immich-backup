// cmd/backup.go
package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
	"github.com/daksh7011/immich-backup/internal/backup"
	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/docker"
	"github.com/daksh7011/immich-backup/internal/doctor"
	"github.com/daksh7011/immich-backup/internal/rcloneconf"
	"github.com/daksh7011/immich-backup/internal/status"
	"github.com/daksh7011/immich-backup/internal/tui"
)

func newBackupCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "backup",
		Short: "Run a backup now",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := GetConfig(cmd)

			// Prerequisite checks — fail fast
			client, err := docker.NewClient()
			if err != nil {
				slog.Error("docker socket unreachable", "error", err,
					"remedy", "ensure Docker is running")
				os.Exit(1)
			}
			defer client.Close()

			results := doctor.Check(client, cfg, config.RcloneConfigPath())
			if doctor.AnyFailed(results) {
				for _, r := range results {
					if !r.OK {
						slog.Error("prerequisite check failed",
							"check", r.Name, "message", r.Message, "remedy", r.Remedy)
					}
				}
				client.Close() // os.Exit bypasses defer
				os.Exit(1)
			}

			// Open log file before registering cancel so the defer order (LIFO) is:
			//   1. cancel()        — signals goroutine to stop writing
			//   2. logFile.Close() — safe to close after goroutine is signalled
			logFile := openRcloneLog(config.RcloneLogPath())
			defer logFile.Close()

			// ctx is cancelled when the user presses Ctrl+C, which kills rclone.
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			skipDB, _ := cmd.Flags().GetBool("skip-db")
			skipMedia, _ := cmd.Flags().GetBool("skip-media")

			if skipDB && skipMedia {
				return fmt.Errorf("--skip-db and --skip-media together would back up nothing")
			}

			// Resolve effective remote: --remote triggers interactive picker, saves the chosen
			// path to remote_paths for future pre-fill but does not update rclone_remote itself.
			effectiveRemote := cfg.Backup.RcloneRemote
			pickRemote, _ := cmd.Flags().GetBool("remote")
			if pickRemote {
				if !isTTY() {
					return fmt.Errorf("--remote requires an interactive terminal")
				}
				remotes, err := rcloneconf.ListRemotes(config.RcloneConfigPath())
				if err != nil || len(remotes) == 0 {
					return fmt.Errorf("no rclone remotes found in %s — run `configure` first", config.RcloneConfigPath())
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
					fmt.Println("Backup cancelled.")
					return nil
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
				config.RcloneConfigPath(),
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

			run := &status.LastRun{Time: time.Now().UTC()}
			if isTTY() {
				model := tui.NewBackupModel(ch, cancel, skipDB, skipMedia)
				p := tea.NewProgram(model)
				result, err := p.Run()
				if err != nil {
					// TUI itself failed — backup outcome unknown; don't save a
					// misleading status record. defer cancel() will stop the goroutine.
					return fmt.Errorf("backup TUI: %w", err)
				}
				final := result.(tui.BackupModel)
				if final.Err() != nil {
					run.Result = "error"
					run.Error = final.Err().Error()
				} else {
					run.Result = "success"
				}
			} else {
				if err := runBackupHeadless(ctx, ch); err != nil {
					run.Result = "error"
					run.Error = err.Error()
					_ = status.Save(config.StatusFilePath(), run)
					return err
				}
				run.Result = "success"
			}
			_ = status.Save(config.StatusFilePath(), run)
			return nil
		},
	}
	c.Flags().Bool("skip-db", false, "Skip database dump and upload")
	c.Flags().Bool("skip-media", false, "Skip media sync")
	c.Flags().Bool("remote", false, "Interactively select backup remote and save path for future pre-fill")
	return c
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
