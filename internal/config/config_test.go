// internal/config/config_test.go
package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daksh7011/immich-backup/internal/config"
)

func TestLoad_CreatesDefaultsWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Backup.RcloneRemote == "" {
		t.Error("expected default rclone_remote to be populated")
	}
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		t.Error("expected config file to be written to disk")
	}
}

func TestLoad_FailsOnMissingUploadLocation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(path, []byte(`
immich:
  postgres_container: immich_postgres
  postgres_user: postgres
  postgres_db: immich
backup:
  rclone_remote: "b2:test"
  schedule: "0 3 * * *"
  db_backup_frequency: "0 */6 * * *"
  retention:
    daily: 7
    weekly: 4
daemon:
  log_path: /tmp/test.log
`), 0644)

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "upload_location") {
		t.Errorf("expected error to mention upload_location, got: %v", err)
	}
}

func TestLoad_FailsOnInvalidCron(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(path, []byte(`
immich:
  upload_location: /mnt/immich
  postgres_container: immich_postgres
  postgres_user: postgres
  postgres_db: immich
backup:
  rclone_remote: "b2:test"
  schedule: "not-a-cron"
  db_backup_frequency: "0 */6 * * *"
  retention:
    daily: 7
    weekly: 4
daemon:
  log_path: /tmp/test.log
`), 0644)

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected validation error for invalid cron")
	}
	if !strings.Contains(err.Error(), "schedule") {
		t.Errorf("expected error to mention schedule, got: %v", err)
	}
}

func TestLoad_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(path, []byte(`
immich:
  upload_location: /mnt/immich
  postgres_container: immich_postgres
  postgres_user: postgres
  postgres_db: immich
backup:
  rclone_remote: "b2:test"
  schedule: "0 3 * * *"
  db_backup_frequency: "0 */6 * * *"
  retention:
    daily: 7
    weekly: 4
daemon:
  log_path: /tmp/test.log
`), 0644)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Immich.UploadLocation != "/mnt/immich" {
		t.Errorf("got upload_location=%q, want /mnt/immich", cfg.Immich.UploadLocation)
	}
	if cfg.Backup.Retention.Daily != 7 {
		t.Errorf("got retention.daily=%d, want 7", cfg.Backup.Retention.Daily)
	}
}

func TestValidate_FailsOnZeroTransfers(t *testing.T) {
	cfg := config.Config{
		Immich: config.ImmichConfig{
			UploadLocation:    "/mnt/immich",
			PostgresContainer: "immich_postgres",
			PostgresUser:      "postgres",
			PostgresDB:        "immich",
		},
		Backup: config.BackupConfig{
			RcloneRemote:      "b2:test",
			Schedule:          "0 3 * * *",
			DBBackupFrequency: "0 */6 * * *",
			Retention:         config.RetentionConfig{Daily: 7, Weekly: 4},
			Transfers:         0, // invalid
			Checkers:          128,
			BufferSize:        "64M",
		},
		Daemon: config.DaemonConfig{LogPath: "/tmp/test.log"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for zero transfers")
	}
	if !strings.Contains(err.Error(), "transfers") {
		t.Errorf("expected error to mention transfers, got: %v", err)
	}
}

func TestValidate_FailsOnZeroCheckers(t *testing.T) {
	cfg := config.Config{
		Immich: config.ImmichConfig{
			UploadLocation:    "/mnt/immich",
			PostgresContainer: "immich_postgres",
			PostgresUser:      "postgres",
			PostgresDB:        "immich",
		},
		Backup: config.BackupConfig{
			RcloneRemote:      "b2:test",
			Schedule:          "0 3 * * *",
			DBBackupFrequency: "0 */6 * * *",
			Retention:         config.RetentionConfig{Daily: 7, Weekly: 4},
			Transfers:         48,
			Checkers:          0, // invalid
			BufferSize:        "64M",
		},
		Daemon: config.DaemonConfig{LogPath: "/tmp/test.log"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for zero checkers")
	}
	if !strings.Contains(err.Error(), "checkers") {
		t.Errorf("expected error to mention checkers, got: %v", err)
	}
}

func TestValidate_FailsOnEmptyBufferSize(t *testing.T) {
	cfg := config.Config{
		Immich: config.ImmichConfig{
			UploadLocation:    "/mnt/immich",
			PostgresContainer: "immich_postgres",
			PostgresUser:      "postgres",
			PostgresDB:        "immich",
		},
		Backup: config.BackupConfig{
			RcloneRemote:      "b2:test",
			Schedule:          "0 3 * * *",
			DBBackupFrequency: "0 */6 * * *",
			Retention:         config.RetentionConfig{Daily: 7, Weekly: 4},
			Transfers:         48,
			Checkers:          128,
			BufferSize:        "", // invalid
		},
		Daemon: config.DaemonConfig{LogPath: "/tmp/test.log"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for empty buffer_size")
	}
	if !strings.Contains(err.Error(), "buffer_size") {
		t.Errorf("expected error to mention buffer_size, got: %v", err)
	}
}

func TestLoad_AppliesDefaultPerfFieldsForLegacyConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// Legacy config without transfers/checkers/buffer_size fields.
	_ = os.WriteFile(path, []byte(`
immich:
  upload_location: /mnt/immich
  postgres_container: immich_postgres
  postgres_user: postgres
  postgres_db: immich
backup:
  rclone_remote: "b2:test"
  schedule: "0 3 * * *"
  db_backup_frequency: "0 */6 * * *"
  retention:
    daily: 7
    weekly: 4
daemon:
  log_path: /tmp/test.log
`), 0644)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Backup.Transfers != 48 {
		t.Errorf("Transfers: got %d, want 48", cfg.Backup.Transfers)
	}
	if cfg.Backup.Checkers != 128 {
		t.Errorf("Checkers: got %d, want 128", cfg.Backup.Checkers)
	}
	if cfg.Backup.BufferSize != "64M" {
		t.Errorf("BufferSize: got %q, want 64M", cfg.Backup.BufferSize)
	}
}

func TestLoad_NewConfigHasPerfDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Backup.Transfers != 48 {
		t.Errorf("Transfers: got %d, want 48", cfg.Backup.Transfers)
	}
	if cfg.Backup.Checkers != 128 {
		t.Errorf("Checkers: got %d, want 128", cfg.Backup.Checkers)
	}
	if cfg.Backup.BufferSize != "64M" {
		t.Errorf("BufferSize: got %q, want 64M", cfg.Backup.BufferSize)
	}
}
