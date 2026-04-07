// cmd/daemon.go
package cmd

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/daksh7011/immich-backup/internal/daemon"
	"github.com/daksh7011/immich-backup/internal/tui"
)

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the immich-backup background service",
	}

	// daemon.New() is called inside RunE, not at construction time, so it
	// does not panic on platforms where the Manager is not yet implemented.
	cmd.AddCommand(newDaemonSubCmd("install", "Install and enable the background service",
		func(c *cobra.Command) error { return daemon.New().Install(GetConfig(c)) }))
	cmd.AddCommand(newDaemonSubCmd("uninstall", "Remove the background service",
		func(c *cobra.Command) error { return daemon.New().Uninstall() }))
	cmd.AddCommand(newDaemonSubCmd("start", "Start the background service",
		func(c *cobra.Command) error { return daemon.New().Start() }))
	cmd.AddCommand(newDaemonSubCmd("stop", "Stop the background service",
		func(c *cobra.Command) error { return daemon.New().Stop() }))
	cmd.AddCommand(newDaemonSubCmd("restart", "Restart the background service",
		func(c *cobra.Command) error { return daemon.New().Restart() }))
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show background service status",
		RunE: func(c *cobra.Command, _ []string) error {
			out, err := daemon.New().Status()
			return runDaemonModel(out, err)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "logs",
		Short: "Show background service logs",
		RunE: func(c *cobra.Command, _ []string) error {
			out, err := daemon.New().Logs()
			if err != nil {
				return err
			}
			_, runErr := tea.NewProgram(tui.NewLogsModel(out)).Run()
			return runErr
		},
	})
	return cmd
}

func newDaemonSubCmd(use, short string, fn func(*cobra.Command) error) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(c *cobra.Command, _ []string) error {
			return runDaemonModel("", fn(c))
		},
	}
}

func runDaemonModel(msg string, err error) error {
	if msg == "" && err == nil {
		msg = "Done."
	}
	_, runErr := tea.NewProgram(tui.NewDaemonModel(msg, err)).Run()
	if runErr != nil {
		return fmt.Errorf("TUI: %w", runErr)
	}
	return err
}
