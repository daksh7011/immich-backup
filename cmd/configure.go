// cmd/configure.go
package cmd

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/rcloneconf"
	"github.com/daksh7011/immich-backup/internal/tui"
)

func newConfigureCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "configure",
		Short: "Re-run the configuration wizard",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rcloneconf.EnsureConfigured(config.RcloneConfigPath()); err != nil {
				fmt.Fprintln(os.Stderr, "rclone setup failed:", err)
				os.Exit(1)
			}
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
