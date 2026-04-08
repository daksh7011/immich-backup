// cmd/doctor.go
package cmd

import (
	tea "charm.land/bubbletea/v2"
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
			cfg, _ := config.Load(config.DefaultConfigPath())
			if cfg == nil {
				cfg = &config.Config{}
			}

			client, err := docker.NewClient()
			if err != nil {
				client = nil
				_ = err
			}
			var ex docker.Executor
			if client != nil {
				ex = client
				defer client.Close()
			}

			ch := make(chan any, 10)
			go func() {
				doctor.CheckAsync(ex, cfg, config.RcloneConfigPath(), ch)
				close(ch)
			}()

			model := tui.NewDoctorModel(ch)
			_, err = tea.NewProgram(model).Run()
			return err
		},
	}
}
