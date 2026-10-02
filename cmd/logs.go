// cmd/logs.go
package cmd

import (
	"fmt"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/daemon"
	"github.com/daksh7011/immich-backup/internal/tui"
	"github.com/spf13/cobra"
)

// daemonLogPath picks the daemon log to show. Only scheduled runs write it,
// so the file named by the installed unit or plist (def, nil when not
// installed) wins over the config's daemon.log_path; when the two differ the
// returned warning says so, since the config changed after `daemon install`.
// cfgLoaded is false when the config could not be read and cfgPath is only
// the default.
func daemonLogPath(cfgPath string, cfgLoaded bool, def *daemon.Definition) (path, warning string) {
	if def == nil || def.LogPath == "" || def.LogPath == cfgPath {
		return cfgPath, ""
	}
	if !cfgLoaded {
		return def.LogPath, ""
	}
	return def.LogPath, fmt.Sprintf("warning: the installed service writes to %s, but daemon.log_path is %s; "+
		"showing the service's log. Run `immich-backup daemon install` to apply the config.", def.LogPath, cfgPath)
}

// printNoLogFile explains a missing log. Only scheduled runs write it, so where
// the platform has a service manager, point at the commands that show whether
// runs are scheduled and why they did not start.
func printNoLogFile(w io.Writer, path string, hasService bool) {
	fmt.Fprintln(w, "No log file found at", path)
	if hasService {
		fmt.Fprintln(w, "No scheduled run has written to it yet. Run `immich-backup daemon status` to see "+
			"whether runs are scheduled and how the last one ended, and `immich-backup daemon logs` "+
			"for the scheduler's own messages.")
	}
}

func newLogsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "logs",
		Short: "Show daemon or rclone log output",
		RunE: func(cmd *cobra.Command, args []string) error {
			rclone, _ := cmd.Flags().GetBool("rclone")

			var logPath string
			var def *daemon.Definition
			hasService := false
			if rclone {
				logPath = config.RcloneLogPath()
			} else {
				// logs is in the PersistentPreRun skip list; load config directly.
				cfgPath, cfgLoaded := config.DefaultLogPath(), false
				if cfg, err := config.Load(config.DefaultConfigPath()); err == nil {
					cfgPath, cfgLoaded = cfg.Daemon.LogPath, true
				}
				// Without a service manager (unsupported OS) or an installed
				// service, the config's path is all there is.
				if m, err := daemon.Detect(); err == nil {
					hasService = true
					def, _ = m.Definition()
				}
				var warning string
				logPath, warning = daemonLogPath(cfgPath, cfgLoaded, def)
				if warning != "" {
					fmt.Fprintln(os.Stderr, warning)
				}
			}

			data, err := daemon.TailFile(logPath, 1<<20) // last 1 MiB
			if err != nil {
				if os.IsNotExist(err) {
					printNoLogFile(os.Stdout, logPath, hasService)
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
	c.Flags().Bool("rclone", false, "Show the rclone log instead of the daemon log")
	return c
}
