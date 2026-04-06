// cmd/backup.go
package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/daksh7011/immich-backup/internal/backup"
	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/docker"
	"github.com/daksh7011/immich-backup/internal/doctor"
	"github.com/daksh7011/immich-backup/internal/status"
	"github.com/daksh7011/immich-backup/internal/tui"
)

func newBackupCmd() *cobra.Command {
	return &cobra.Command{
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

			// Start live TUI + backup runner concurrently
			ch := make(chan any, 16)
			go backup.Run(
				config.RcloneConfigPath(),
				cfg.Immich.PostgresContainer,
				cfg.Immich.PostgresUser,
				cfg.Immich.PostgresDB,
				cfg.Immich.UploadLocation,
				cfg.Backup.RcloneRemote,
				client,
				ch,
			)

			model := tui.NewBackupModel(ch)
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
}
