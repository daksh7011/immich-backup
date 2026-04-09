// cmd/daemon.go
package cmd

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
	"github.com/daksh7011/immich-backup/internal/daemon"
	"github.com/daksh7011/immich-backup/internal/tui"
)

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the immich-backup background service",
	}

	cmd.AddCommand(newDaemonSubCmd("install", "Install and enable the background service", "Installing service…",
		func(c *cobra.Command) error { return daemon.New().Install(GetConfig(c)) }))
	cmd.AddCommand(newDaemonSubCmd("uninstall", "Remove the background service", "Uninstalling service…",
		func(c *cobra.Command) error { return daemon.New().Uninstall() }))
	cmd.AddCommand(newDaemonSubCmd("start", "Start the background service", "Starting service…",
		func(c *cobra.Command) error { return daemon.New().Start() }))
	cmd.AddCommand(newDaemonSubCmd("stop", "Stop the background service", "Stopping service…",
		func(c *cobra.Command) error { return daemon.New().Stop() }))
	cmd.AddCommand(newDaemonSubCmd("restart", "Restart the background service", "Restarting service…",
		func(c *cobra.Command) error { return daemon.New().Restart() }))

	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show background service status",
		RunE: func(c *cobra.Command, _ []string) error {
			ch := make(chan any, 1)
			go func() {
				out, err := daemon.New().Status()
				ch <- tui.DaemonResultMsg{Msg: out, Err: err}
				close(ch)
			}()
			result, runErr := tea.NewProgram(tui.NewDaemonModel(ch, "Fetching service status…")).Run()
			if runErr != nil {
				return fmt.Errorf("TUI: %w", runErr)
			}
			return result.(tui.DaemonModel).Err()
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

// newDaemonSubCmd creates a daemon subcommand that runs fn in a goroutine and
// shows a spinner (label) while waiting.
func newDaemonSubCmd(use, short, label string, fn func(*cobra.Command) error) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(c *cobra.Command, _ []string) error {
			ch := make(chan any, 1)
			go func() {
				err := fn(c)
				msg := ""
				if err == nil {
					msg = "Done."
				}
				ch <- tui.DaemonResultMsg{Msg: msg, Err: err}
				close(ch)
			}()
			result, runErr := tea.NewProgram(tui.NewDaemonModel(ch, label)).Run()
			if runErr != nil {
				return fmt.Errorf("TUI: %w", runErr)
			}
			return result.(tui.DaemonModel).Err()
		},
	}
}
