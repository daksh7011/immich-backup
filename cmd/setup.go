// cmd/setup.go
package cmd

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/tui"
	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Interactive first-run configuration wizard",
		RunE: func(cmd *cobra.Command, args []string) error {
			promptRcloneConfig(config.RcloneConfigPath())

			// Load the existing config, or defaults if missing, without
			// validating it, so setup can repair an invalid file.
			path := config.DefaultConfigPath()
			cfg, err := loadWizardConfig(path)
			if err != nil {
				return err
			}
			oldSchedule := cfg.Backup.Schedule

			model := tui.NewSetupModel(cfg, config.RcloneConfigPath())
			p := tea.NewProgram(model)
			result, err := p.Run()
			if err != nil {
				return fmt.Errorf("setup wizard: %w", err)
			}
			final := result.(tui.SetupModel)
			if !final.Done() {
				fmt.Println("Setup cancelled.")
				return nil
			}

			return finishWizard(path, oldSchedule, final.Result(), "Configuration saved to "+path)
		},
	}
}
