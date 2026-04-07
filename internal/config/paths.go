// internal/config/paths.go
package config

import (
	"os"
	"path/filepath"
)

// AppDir returns ~/.immich-backup.
func AppDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".immich-backup")
}

func DefaultConfigPath() string { return filepath.Join(AppDir(), "config.yaml") }
func RcloneConfigPath() string  { return filepath.Join(AppDir(), "rclone.conf") }
func StatusFilePath() string    { return filepath.Join(AppDir(), "last-run.json") }
func DefaultLogPath() string    { return filepath.Join(AppDir(), "logs", "daemon.log") }
