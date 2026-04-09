// cmd/logs.go
package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/tui"
)

// tailFile reads the last maxBytes bytes of the named file.
// If the file is larger than maxBytes, the result is trimmed to start on a
// line boundary so no partial log lines are returned.
func tailFile(name string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}

	start := size - maxBytes
	if start < 0 {
		start = 0
	}
	if _, err = f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}

	buf, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	// Drop the first (possibly partial) line when we seeked into the middle.
	if start > 0 {
		if i := bytes.IndexByte(buf, '\n'); i >= 0 {
			buf = buf[i+1:]
		}
	}
	return buf, nil
}

func newLogsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "logs",
		Short: "Show daemon or rclone log output",
		RunE: func(cmd *cobra.Command, args []string) error {
			rclone, _ := cmd.Flags().GetBool("rclone")

			var logPath string
			if rclone {
				logPath = config.RcloneLogPath()
			} else {
				// logs is in the PersistentPreRun skip list; load config directly.
				logPath = config.DefaultLogPath()
				if cfg, err := config.Load(config.DefaultConfigPath()); err == nil {
					logPath = cfg.Daemon.LogPath
				}
			}

			data, err := tailFile(logPath, 1<<20) // last 1 MiB
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
	c.Flags().Bool("rclone", false, "Show rclone debug log instead of daemon log")
	return c
}
