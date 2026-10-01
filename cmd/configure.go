// cmd/configure.go
package cmd

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/tui"
	"github.com/spf13/cobra"
)

func newConfigureCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "configure",
		Short: "Re-run the configuration wizard",
		RunE: func(cmd *cobra.Command, args []string) error {
			promptRcloneConfig(config.RcloneConfigPath())

			path := config.DefaultConfigPath()
			cfg, err := loadWizardConfig(path)
			if err != nil {
				return err
			}
			oldSchedule := cfg.Backup.Schedule

			model := tui.NewConfigureModel(cfg, config.RcloneConfigPath())
			p := tea.NewProgram(model)
			result, err := p.Run()
			if err != nil {
				return fmt.Errorf("configure wizard: %w", err)
			}
			final := result.(tui.ConfigureModel)
			if !final.Done() {
				fmt.Println("Configure cancelled.")
				return nil
			}
			return finishWizard(path, oldSchedule, final.Result(), "Configuration updated.")
		},
	}
}
