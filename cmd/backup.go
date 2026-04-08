// cmd/backup.go
package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
	"github.com/daksh7011/immich-backup/internal/backup"
	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/docker"
	"github.com/daksh7011/immich-backup/internal/doctor"
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
				os.Exit(1)
			}

			// Start live TUI + backup runner concurrently.
			// ctx is cancelled when the user presses Ctrl+C, which kills rclone.
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			skipDB, _ := cmd.Flags().GetBool("skip-db")
			skipMedia, _ := cmd.Flags().GetBool("skip-media")

			logFile := openRcloneLog(config.RcloneLogPath())
			defer logFile.Close()

			ch := make(chan any, 16)
			go backup.Run(
				ctx,
				config.RcloneConfigPath(),
				cfg.Immich.PostgresContainer,
				cfg.Immich.PostgresUser,
				cfg.Immich.UploadLocation,
				cfg.Backup.RcloneRemote,
				client,
				skipDB, skipMedia,
				logFile,
				ch,
			)

			model := tui.NewBackupModel(ch, cancel, skipDB, skipMedia)
			p := tea.NewProgram(model)
			result, err := p.Run()
			if err != nil {
				return fmt.Errorf("backup TUI: %w", err)
			}

			final := result.(tui.BackupModel)
			run := &status.LastRun{Time: time.Now().UTC()}
			if final.Err() != nil {
				run.Result = "error"
				run.Error = final.Err().Error()
			} else {
				run.Result = "success"
			}
			_ = status.Save(config.StatusFilePath(), run)
			return nil
		},
	}
	c.Flags().Bool("skip-db", false, "Skip database dump and upload")
	c.Flags().Bool("skip-media", false, "Skip media sync")
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
