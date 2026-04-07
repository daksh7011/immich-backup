// cmd/setup.go
package cmd

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/rcloneconf"
	"github.com/daksh7011/immich-backup/internal/tui"
)

func newSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Interactive first-run configuration wizard",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Ensure rclone is configured first (pause-launch-resume)
			if err := rcloneconf.EnsureConfigured(config.RcloneConfigPath()); err != nil {
				fmt.Fprintln(os.Stderr, "rclone setup failed:", err)
				os.Exit(1)
			}

			// Load existing config (creates defaults if missing)
			cfg, err := config.Load(config.DefaultConfigPath())
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// Run the setup wizard
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
