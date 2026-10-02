// cmd/status.go
package cmd

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/daemon"
	"github.com/daksh7011/immich-backup/internal/status"
	"github.com/daksh7011/immich-backup/internal/tui"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show last backup result and next scheduled run",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := GetConfig(cmd)
			run, _ := status.Load(config.StatusFilePath()) // nil if no backup yet
			var svc stateReader
			m, err := daemon.Detect()
			if err == nil {
				svc = m
			}
			model := tui.NewStatusModel(run, serviceInfo(svc, err, cfg.Backup.Schedule))
			p := tea.NewProgram(model)
			_, err = p.Run()
			return err
		},
	}
}

// stateReader is the part of daemon.Manager the status screen needs.
type stateReader interface {
	State() (daemon.State, error)
}

// serviceInfo describes the background service for the status screen. svc is
// nil, with detectErr set, on platforms without a service manager; status
// then still shows the configured schedule rather than failing.
func serviceInfo(svc stateReader, detectErr error, schedule string) tui.ServiceInfo {
	sched := fmt.Sprintf("(schedule: %s)", schedule)
	if svc == nil {
		msg := "unavailable"
		if detectErr != nil {
			msg += ": " + detectErr.Error()
		}
		return tui.ServiceInfo{State: msg, NextRun: sched}
	}
	st, err := svc.State()
	if err != nil {
		return tui.ServiceInfo{State: "unknown", NextRun: sched, Problems: []string{err.Error()}}
	}
	info := tui.ServiceInfo{State: st.Status, NextRun: st.NextRun, Problems: st.Problems}
	switch {
	case !st.Active:
		info.NextRun = "none: no backups are scheduled " + sched
	case info.NextRun == "":
		info.NextRun = sched
	}
	return info
}
