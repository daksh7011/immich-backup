// internal/config/config.go
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Immich ImmichConfig `yaml:"immich"`
	Backup BackupConfig `yaml:"backup"`
	Daemon DaemonConfig `yaml:"daemon"`
}

type ImmichConfig struct {
	UploadLocation    string `yaml:"upload_location"`
	PostgresContainer string `yaml:"postgres_container"`
	PostgresUser      string `yaml:"postgres_user"`
	PostgresDB        string `yaml:"postgres_db"`
}

type BackupConfig struct {
	RcloneRemote string `yaml:"rclone_remote"`
	// Schedule is the daily run time, "MINUTE HOUR * * *"; see ParseDailySchedule.
	Schedule string `yaml:"schedule"`
	// DBBackupFrequency and Retention are not implemented yet: nothing reads
	// them. They are kept so existing configs still load and round-trip, and
	// omitted from new configs so they are not advertised.
	DBBackupFrequency string            `yaml:"db_backup_frequency,omitempty"`
	Retention         RetentionConfig   `yaml:"retention,omitempty"`
	Transfers         int               `yaml:"transfers"`
	Checkers          int               `yaml:"checkers"`
	BufferSize        string            `yaml:"buffer_size"`
	RemotePaths       map[string]string `yaml:"remote_paths"`
}

type RetentionConfig struct {
	Daily  int `yaml:"daily"`
	Weekly int `yaml:"weekly"`
}

type DaemonConfig struct {
	LogPath string `yaml:"log_path"`
}

var defaults = Config{
	Immich: ImmichConfig{
		UploadLocation:    "/mnt/immich",
		PostgresContainer: "immich_postgres",
		PostgresUser:      "postgres",
		PostgresDB:        "immich",
	},
	Backup: BackupConfig{
		RcloneRemote: "b2-encrypted:immich-backup",
		Schedule:     "0 3 * * *",
		Transfers:    48,
		Checkers:     128,
		BufferSize:   "64M",
	},
}

// Load reads the config at path. If missing, writes defaults and returns them.
// If present, unmarshals, fills in missing perf fields with defaults, then validates.
func Load(path string) (*Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := defaults
		cfg.Daemon.LogPath = DefaultLogPath()
		if err := Save(path, &cfg); err != nil {
			return nil, fmt.Errorf("write default config: %w", err)
		}
		return &cfg, nil
	}
	cfg, err := LoadRaw(path)
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// LoadRaw reads the config at path and fills in defaults like Load, but does
// not validate it and never writes it; a missing file yields the defaults.
// setup and configure use it so they can open, and repair, a config that
// Load rejects. Only a file that cannot be read or parsed is an error.
func LoadRaw(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		cfg := defaults
		applyDefaults(&cfg)
		return &cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	applyDefaults(&cfg)
	return &cfg, nil
}

// applyDefaults fills in zero-value perf fields with built-in defaults.
// This ensures legacy configs (without transfers/checkers/buffer_size) load cleanly.
// It also defaults daemon.log_path and expands a leading "~" in path fields:
// systemd and launchd take the log path literally and never expand "~".
func applyDefaults(cfg *Config) {
	if cfg.Daemon.LogPath == "" {
		cfg.Daemon.LogPath = DefaultLogPath()
	}
	cfg.Daemon.LogPath = expandHome(cfg.Daemon.LogPath)
	cfg.Immich.UploadLocation = expandHome(cfg.Immich.UploadLocation)
	if cfg.Backup.Transfers == 0 {
		cfg.Backup.Transfers = defaults.Backup.Transfers
	}
	if cfg.Backup.Checkers == 0 {
		cfg.Backup.Checkers = defaults.Backup.Checkers
	}
	if cfg.Backup.BufferSize == "" {
		cfg.Backup.BufferSize = defaults.Backup.BufferSize
	}
}

// Save marshals cfg to YAML and writes it to path, creating parent dirs as needed.
func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// Validate checks all required fields and the schedule. db_backup_frequency
// and retention are not checked: they are not implemented yet.
func (c *Config) Validate() error {
	var errs []string
	if c.Immich.UploadLocation == ""    { errs = append(errs, "immich.upload_location is required") }
	if c.Immich.PostgresContainer == "" { errs = append(errs, "immich.postgres_container is required") }
	if c.Immich.PostgresUser == ""      { errs = append(errs, "immich.postgres_user is required") }
	if c.Immich.PostgresDB == ""        { errs = append(errs, "immich.postgres_db is required") }
	if c.Backup.RcloneRemote == ""      { errs = append(errs, "backup.rclone_remote is required") }
	if c.Backup.Schedule == ""          { errs = append(errs, "backup.schedule is required") }
	if c.Backup.Schedule != "" {
		if err := ValidateDaemonSchedule(c.Backup.Schedule); err != nil {
			errs = append(errs, "backup.schedule: "+err.Error())
		}
	}
	if c.Backup.Transfers <= 0        { errs = append(errs, "backup.transfers must be > 0") }
	if c.Backup.Checkers <= 0         { errs = append(errs, "backup.checkers must be > 0") }
	if c.Backup.BufferSize == ""       { errs = append(errs, "backup.buffer_size is required") }
	if c.Daemon.LogPath == ""          { errs = append(errs, "daemon.log_path is required") }
	if c.Daemon.LogPath != "" && !isAbsPath(c.Daemon.LogPath) {
		errs = append(errs, fmt.Sprintf("daemon.log_path must be an absolute path (got %q)", c.Daemon.LogPath))
	}
	if len(errs) > 0 {
		return fmt.Errorf("config validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

// expandHome replaces a leading "~" or "~/" in p with the user's home dir.
// "~user/..." forms are left alone (and then rejected as relative by Validate).
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~"))
}

// isAbsPath reports whether p is absolute. A leading "/" always counts, so a
// config written for the Linux/macOS host also validates when tests run on Windows.
func isAbsPath(p string) bool {
	return filepath.IsAbs(p) || strings.HasPrefix(p, "/")
}
