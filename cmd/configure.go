// cmd/configure.go
package cmd

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/tui"
)

func newConfigureCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "configure",
		Short: "Re-run the configuration wizard",
		RunE: func(cmd *cobra.Command, args []string) error {
			promptRcloneConfig(config.RcloneConfigPath())

			cfg, err := config.Load(config.DefaultConfigPath())
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			model := tui.NewConfigureModel(cfg)
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
			if err := config.Save(config.DefaultConfigPath(), final.Result()); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			fmt.Println("Configuration updated.")
			return nil
		},
	}
}
