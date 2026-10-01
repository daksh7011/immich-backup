// cmd/doctor.go
package cmd

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/daemon"
	"github.com/daksh7011/immich-backup/internal/docker"
	"github.com/daksh7011/immich-backup/internal/doctor"
	"github.com/daksh7011/immich-backup/internal/tui"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check all prerequisites and display results",
		RunE: func(cmd *cobra.Command, args []string) error {
			// A load error is shown as the failed Config check; the empty
			// fallback only keeps the other checks running.
			cfg, cfgErr := config.Load(config.DefaultConfigPath())
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

			// The background service checks only warn; on a platform without a
			// service manager svc stays nil and they say so.
			var svc doctor.Service
			if m, err := daemon.Detect(); err == nil {
				svc = m
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			ch := make(chan any, 10)
			go func() {
				doctor.CheckAsync(ctx, ex, cfg, cfgErr, config.RcloneConfigPath(), svc, ch)
				close(ch)
			}()

			model := tui.NewDoctorModel(ch)
			result, err := tea.NewProgram(model).Run()
			if err != nil {
				return err
			}
			if result.(tui.DoctorModel).AnyFailed() {
				return fmt.Errorf("one or more prerequisite checks failed")
			}
			return nil
		},
	}
}
