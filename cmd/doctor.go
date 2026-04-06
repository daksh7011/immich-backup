// cmd/doctor.go
package cmd

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/docker"
	"github.com/daksh7011/immich-backup/internal/doctor"
	"github.com/daksh7011/immich-backup/internal/tui"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check all prerequisites and display results",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Doctor loads config itself — it must run even when config is invalid.
			// A nil cfg means config check will fail, which is reported as a CheckResult.
			cfg, _ := config.Load(config.DefaultConfigPath())
			if cfg == nil {
				cfg = &config.Config{}
			}

			client, err := docker.NewClient()
			if err != nil {
				// Doctor must still run even if Docker is down — report it
				client = nil
				_ = err
			}
			var ex docker.Executor
			if client != nil {
				ex = client
				defer client.Close()
			}

			results := doctor.Check(ex, cfg, config.RcloneConfigPath())
			model := tui.NewDoctorModel(results)
			p := tea.NewProgram(model)
			_, err = p.Run()
			return err
		},
	}
}
