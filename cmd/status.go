// cmd/status.go
package cmd

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/status"
	"github.com/daksh7011/immich-backup/internal/tui"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show last backup result and next scheduled run",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := GetConfig(cmd)
			run, _ := status.Load(config.StatusFilePath()) // nil if no backup yet
			nextRun := fmt.Sprintf("(schedule: %s)", cfg.Backup.Schedule)
			model := tui.NewStatusModel(run, nextRun)
			p := tea.NewProgram(model)
			_, err := p.Run()
			return err
		},
	}
}
