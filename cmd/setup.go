// cmd/setup.go
package cmd

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/tui"
)

func newSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Interactive first-run configuration wizard",
		RunE: func(cmd *cobra.Command, args []string) error {
			promptRcloneConfig(config.RcloneConfigPath())

			// Load existing config (creates defaults if missing)
			cfg, err := config.Load(config.DefaultConfigPath())
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			model := tui.NewSetupModel(cfg)
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

			if err := config.Save(config.DefaultConfigPath(), final.Result()); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			fmt.Println("Configuration saved to", config.DefaultConfigPath())
			return nil
		},
	}
}
