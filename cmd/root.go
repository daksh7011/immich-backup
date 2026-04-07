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
// Exits with a clear error if called on a command that bypasses PersistentPreRunE.
func GetConfig(cmd *cobra.Command) *config.Config {
	v := cmd.Context().Value(contextKey{})
	if v == nil {
		fmt.Fprintln(os.Stderr, "internal error: config not available for this command; ensure it is not in the skip list")
		os.Exit(1)
	}
	return v.(*config.Config)
}

var rootCmd = &cobra.Command{
	Use:   "immich-backup",
	Short: "Back up your Immich media library using rclone",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Commands that load config themselves or need no config.
		// Use full CommandPath to avoid ambiguity (e.g. "status" vs "daemon status").
		skipPaths := map[string]bool{
			"immich-backup setup":            true,
			"immich-backup configure":        true,
			"immich-backup doctor":           true,
			"immich-backup logs":             true,
			"immich-backup daemon uninstall": true,
			"immich-backup daemon start":     true,
			"immich-backup daemon stop":      true,
			"immich-backup daemon restart":   true,
			"immich-backup daemon status":    true,
			"immich-backup daemon logs":      true,
		}
		if skipPaths[cmd.CommandPath()] {
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
	printBanner()
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
