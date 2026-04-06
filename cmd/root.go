// cmd/root.go
package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"github.com/daksh7011/immich-backup/internal/config"
)

type contextKey struct{}

// GetConfig retrieves the loaded config from the command's context.
// Panics if called on a command that does not go through PersistentPreRun.
func GetConfig(cmd *cobra.Command) *config.Config {
	return cmd.Context().Value(contextKey{}).(*config.Config)
}

var rootCmd = &cobra.Command{
	Use:   "immich-backup",
	Short: "Back up your Immich media library using rclone",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// setup, configure, doctor, and logs load config themselves or don't need it.
		skip := map[string]bool{
			"setup":     true,
			"configure": true,
			"doctor":    true,
			"logs":      true,
		}
		if skip[cmd.Name()] {
			return nil
		}
		cfg, err := config.Load(config.DefaultConfigPath())
		if err != nil {
			slog.Error("config error", "error", err,
				"remedy", "run `immich-backup configure` or edit ~/.immich-backup/config.yaml")
			os.Exit(1)
		}
		cmd.SetContext(context.WithValue(cmd.Context(), contextKey{}, cfg))
		return nil
	},
}

// Execute is the entry point called from main.go.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(
		newSetupCmd(),
		newConfigureCmd(),
		newBackupCmd(),
		newStatusCmd(),
		newDoctorCmd(),
		newLogsCmd(),
		newDaemonCmd(),
	)
}
