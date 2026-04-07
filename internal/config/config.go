// internal/config/config.go
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/robfig/cron/v3"
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
	RcloneRemote      string          `yaml:"rclone_remote"`
	Schedule          string          `yaml:"schedule"`
	DBBackupFrequency string          `yaml:"db_backup_frequency"`
	Retention         RetentionConfig `yaml:"retention"`
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
		RcloneRemote:      "b2-encrypted:immich-backup",
		Schedule:          "0 3 * * *",
		DBBackupFrequency: "0 */6 * * *",
		Retention:         RetentionConfig{Daily: 7, Weekly: 4},
	},
}

// Load reads the config at path. If missing, writes defaults and returns them.
// If present, unmarshals and validates. Any validation error is returned as-is.
func Load(path string) (*Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := defaults
		cfg.Daemon.LogPath = DefaultLogPath()
		if err := Save(path, &cfg); err != nil {
			return nil, fmt.Errorf("write default config: %w", err)
		}
		return &cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
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

// Validate checks all required fields and cron expressions.
func (c *Config) Validate() error {
	var errs []string
	if c.Immich.UploadLocation == ""    { errs = append(errs, "immich.upload_location is required") }
	if c.Immich.PostgresContainer == "" { errs = append(errs, "immich.postgres_container is required") }
	if c.Immich.PostgresUser == ""      { errs = append(errs, "immich.postgres_user is required") }
	if c.Immich.PostgresDB == ""        { errs = append(errs, "immich.postgres_db is required") }
	if c.Backup.RcloneRemote == ""      { errs = append(errs, "backup.rclone_remote is required") }
	if c.Backup.Schedule == ""          { errs = append(errs, "backup.schedule is required") }
	if c.Backup.Schedule != "" && !validCron(c.Backup.Schedule) {
		errs = append(errs, "backup.schedule is not a valid cron expression")
	}
	if c.Backup.DBBackupFrequency == "" { errs = append(errs, "backup.db_backup_frequency is required") }
	if c.Backup.DBBackupFrequency != "" && !validCron(c.Backup.DBBackupFrequency) {
		errs = append(errs, "backup.db_backup_frequency is not a valid cron expression")
	}
	if c.Backup.Retention.Daily <= 0  { errs = append(errs, "backup.retention.daily must be > 0") }
	if c.Backup.Retention.Weekly <= 0 { errs = append(errs, "backup.retention.weekly must be > 0") }
	if c.Daemon.LogPath == ""          { errs = append(errs, "daemon.log_path is required") }
	if len(errs) > 0 {
		return fmt.Errorf("config validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

func validCron(expr string) bool {
	p := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := p.Parse(expr)
	return err == nil
}
