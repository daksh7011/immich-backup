// cmd/root.go
package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/spf13/cobra"
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
	// Execute prints the returned error once; a runtime failure is not misuse,
	// so the usage text would only bury it (and fill the daemon log).
	SilenceUsage:  true,
	SilenceErrors: true,
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
		cfg, err := loadCommandConfig(cmd.CommandPath(), config.DefaultConfigPath(), config.StatusFilePath())
		if err != nil {
			return err
		}
		cmd.SetContext(context.WithValue(cmd.Context(), contextKey{}, cfg))
		return nil
	},
}

// loadCommandConfig loads the config for the command at cmdPath. A load failure
// is logged with a remedy and, for `backup`, recorded as a failed run so a
// scheduled backup that cannot even read its config shows up in `status`.
func loadCommandConfig(cmdPath, configPath, statusPath string) (*config.Config, error) {
	cfg, err := config.Load(configPath)
	if err == nil {
		return cfg, nil
	}
	slog.Error("config error", "error", err,
		"remedy", "run `immich-backup configure` or edit ~/.immich-backup/config.yaml")
	err = fmt.Errorf("config error: %w", err)
	if cmdPath == "immich-backup backup" {
		recordRun(statusPath, time.Now().UTC(), err)
	}
	return nil, err
}

// Execute is the entry point called from main.go.
func Execute() {
	maybePrintBanner(os.Stdout, stdoutIsTerminal)
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// maybePrintBanner prints the banner only for an interactive terminal. Under
// systemd or launchd stdout is the daemon log, where it would bury the log lines.
func maybePrintBanner(w io.Writer, isTerminal func() bool) {
	if isTerminal() {
		printBanner(w)
	}
}

// stdoutIsTerminal reports whether stdout is a terminal (not a file or pipe).
func stdoutIsTerminal() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
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
