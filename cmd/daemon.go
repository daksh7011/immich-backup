// cmd/daemon.go
package cmd

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/daksh7011/immich-backup/internal/daemon"
	"github.com/daksh7011/immich-backup/internal/tui"
	"github.com/spf13/cobra"
)

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the immich-backup background service",
	}

	cmd.AddCommand(interruptsOnMac(newDaemonSubCmd("install", "Install and enable the background service", "Installing service…",
		func(c *cobra.Command, m daemon.Manager) error { return m.Install(GetConfig(c)) })))
	cmd.AddCommand(interruptsOnMac(newDaemonSubCmd("uninstall", "Remove the background service", "Uninstalling service…",
		func(c *cobra.Command, m daemon.Manager) error { return m.Uninstall() })))
	cmd.AddCommand(newDaemonSubCmd("start", "Start the background service", "Starting service…",
		func(c *cobra.Command, m daemon.Manager) error { return m.Start() }))
	cmd.AddCommand(interruptsOnMac(newDaemonSubCmd("stop", "Stop the background service", "Stopping service…",
		func(c *cobra.Command, m daemon.Manager) error { return m.Stop() })))
	cmd.AddCommand(interruptsOnMac(newDaemonSubCmd("restart", "Restart the background service", "Restarting service…",
		func(c *cobra.Command, m daemon.Manager) error { return m.Restart() })))

	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show the schedule, next run and last scheduled run",
		Long: "Show the schedule, next run and last scheduled run.\n\n" +
			"Exits non-zero when scheduled backups will not run (not installed, timer stopped, " +
			"lingering off) or the last scheduled run failed.",
		RunE: func(c *cobra.Command, _ []string) error {
			ch := make(chan any, 1)
			go func() {
				var out string
				m, err := daemon.Detect()
				if err == nil {
					out, err = m.Status()
				}
				ch <- tui.DaemonResultMsg{Msg: out, Err: err}
				close(ch)
			}()
			result, runErr := tea.NewProgram(tui.NewDaemonModel(ch, "Background service")).Run()
			if runErr != nil {
				return fmt.Errorf("TUI: %w", runErr)
			}
			return daemonResultErr(result)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "logs",
		Short: "Show the scheduler's messages and the scheduled run's log",
		RunE: func(c *cobra.Command, _ []string) error {
			m, err := daemon.Detect()
			if err != nil {
				return err
			}
			out, err := m.Logs()
			if err != nil {
				return err
			}
			_, runErr := tea.NewProgram(tui.NewLogsModel(out)).Run()
			return runErr
		},
	})

	return cmd
}

// interruptsOnMac documents that the command unloads the launchd job, which
// stops a backup that is running right now. On Linux these commands act only
// on the timer, so a running backup finishes.
func interruptsOnMac(c *cobra.Command) *cobra.Command {
	c.Long = c.Short + ".\n\nOn macOS this unloads the launchd job, which interrupts a backup " +
		"that is running at that moment; on Linux a running backup is left to finish."
	return c
}

// newDaemonSubCmd creates a daemon subcommand that runs fn with the platform's
// service manager in a goroutine and shows a spinner (label) while waiting.
// On a platform without one it fails with daemon.ErrUnsupported.
func newDaemonSubCmd(use, short, label string, fn func(*cobra.Command, daemon.Manager) error) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(c *cobra.Command, _ []string) error {
			ch := make(chan any, 1)
			go func() {
				m, err := daemon.Detect()
				if err == nil {
					err = fn(c, m)
				}
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
			return daemonResultErr(result)
		},
	}
}

// daemonResultErr returns the operation's error as a shownError: the step
// already displays it, so Execute only sets the exit status.
func daemonResultErr(result tea.Model) error {
	if err := result.(tui.DaemonModel).Err(); err != nil {
		return shownError{err}
	}
	return nil
}
