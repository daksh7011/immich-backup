// cmd/logs.go
package cmd

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/tui"
)

func newLogsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logs",
		Short: "Show daemon log output",
		RunE: func(cmd *cobra.Command, args []string) error {
			// logs is in the PersistentPreRun skip list; load config directly.
			logPath := config.DefaultLogPath()
			if cfg, err := config.Load(config.DefaultConfigPath()); err == nil {
				logPath = cfg.Daemon.LogPath
			}

			data, err := os.ReadFile(logPath)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Println("No log file found at", logPath)
					return nil
				}
				return fmt.Errorf("read log: %w", err)
			}
			model := tui.NewLogsModel(string(data))
			p := tea.NewProgram(model)
			_, err = p.Run()
			return err
		},
	}
}
