package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daksh7011/immich-backup/internal/config"
)

func TestValidateDaemonSchedule_Accepts(t *testing.T) {
	for _, expr := range []string{"0 3 * * *", "30 23 * * *", "05 07 * * *", "  0 3 * * *  "} {
		if err := config.ValidateDaemonSchedule(expr); err != nil {
			t.Errorf("%q: unexpected error: %v", expr, err)
		}
	}
}

func TestValidateDaemonSchedule_Rejects(t *testing.T) {
	tests := []struct {
		expr string
		want string // substring of the error
	}{
		{"", "empty"},
		{"0 3 * * 0", "day-of-week"},     // weekly would silently become daily
		{"0 3 1 * *", "day-of-month"},    // monthly
		{"0 3 * 6 *", "month"},           // June only
		{"0 */6 * * *", "hour"},          // step
		{"*/15 3 * * *", "minute"},       // step
		{"0 1-5 * * *", "hour"},          // range
		{"0,30 3 * * *", "minute"},       // list
		{"@daily", "5 cron fields"},      // macro
		{"0 3 * *", "5 cron fields"},     // too few fields
		{"0 3 * * * *", "5 cron fields"}, // seconds field
		{"60 3 * * *", "minute"},
		{"0 24 * * *", "hour"},
		{"-1 3 * * *", "minute"},
		{"+5 3 * * *", "minute"},
		{"not-a-cron", "5 cron fields"},
	}
	for _, tc := range tests {
		err := config.ValidateDaemonSchedule(tc.expr)
		if err == nil {
			t.Errorf("%q: expected error, got nil", tc.expr)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: error %q does not mention %q", tc.expr, err, tc.want)
		}
	}
}

func TestParseDailySchedule(t *testing.T) {
	hour, minute, err := config.ParseDailySchedule("05 07 * * *")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hour != 7 || minute != 5 {
		t.Errorf("got %02d:%02d, want 07:05", hour, minute)
	}
}

func validConfig() config.Config {
	return config.Config{
		Immich: config.ImmichConfig{
			UploadLocation: "/mnt/immich", PostgresContainer: "c",
			PostgresUser: "u", PostgresDB: "d",
		},
		Backup: config.BackupConfig{
			RcloneRemote: "b2:test", Schedule: "0 3 * * *",
			Transfers: 1, Checkers: 1, BufferSize: "64M",
		},
		Daemon: config.DaemonConfig{LogPath: "/tmp/test.log"},
	}
}

func TestValidate_RejectsScheduleTheDaemonCannotInstall(t *testing.T) {
	for _, expr := range []string{"0 3 * * 0", "0 */6 * * *", "@daily"} {
		cfg := validConfig()
		cfg.Backup.Schedule = expr
		err := cfg.Validate()
		if err == nil {
			t.Errorf("%q: expected validation error", expr)
			continue
		}
		if !strings.Contains(err.Error(), "backup.schedule") {
			t.Errorf("%q: expected error to name backup.schedule, got: %v", expr, err)
		}
	}
}

func TestValidate_DBFrequencyAndRetentionNotRequired(t *testing.T) {
	cfg := validConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config without db_backup_frequency/retention should validate: %v", err)
	}
}

func TestLoad_ConfigWithoutDBFrequencyOrRetention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	_ = os.WriteFile(path, []byte(`
immich:
  upload_location: /mnt/immich
  postgres_container: immich_postgres
  postgres_user: postgres
  postgres_db: immich
backup:
  rclone_remote: "b2:test"
  schedule: "0 3 * * *"
daemon:
  log_path: /tmp/test.log
`), 0644)
	if _, err := config.Load(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_LegacyDBFrequencyAndRetentionStillLoadAndRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
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
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, _ := os.ReadFile(path)
	for _, want := range []string{"db_backup_frequency: 0 */6 * * *", "daily: 7", "weekly: 4"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("saved config lost %q:\n%s", want, data)
		}
	}
}

func TestLoad_NewConfigDoesNotAdvertiseUnimplementedSettings(t *testing.T) {
	setHome(t, t.TempDir())
	path := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := config.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	data, _ := os.ReadFile(path)
	for _, unwanted := range []string{"db_backup_frequency", "retention"} {
		if strings.Contains(string(data), unwanted) {
			t.Errorf("default config should not contain %q:\n%s", unwanted, data)
		}
	}
}

func TestLoadRaw_InvalidConfigStillLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	_ = os.WriteFile(path, []byte(`
immich:
  upload_location: ""
backup:
  rclone_remote: "b2:test"
  schedule: "0 */6 * * *"
daemon:
  log_path: ~/logs/daemon.log
`), 0644)
	setHome(t, t.TempDir())

	if _, err := config.Load(path); err == nil {
		t.Fatal("precondition: Load should reject this config")
	}
	cfg, err := config.LoadRaw(path)
	if err != nil {
		t.Fatalf("LoadRaw: %v", err)
	}
	if cfg.Backup.Schedule != "0 */6 * * *" {
		t.Errorf("schedule: got %q, want the file's value", cfg.Backup.Schedule)
	}
	if cfg.Backup.Transfers != 48 {
		t.Errorf("defaults not applied: transfers=%d", cfg.Backup.Transfers)
	}
	if !filepath.IsAbs(cfg.Daemon.LogPath) {
		t.Errorf("log_path not expanded: %q", cfg.Daemon.LogPath)
	}
}

func TestLoadRaw_MissingFileReturnsDefaultsWithoutWriting(t *testing.T) {
	setHome(t, t.TempDir())
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg, err := config.LoadRaw(path)
	if err != nil {
		t.Fatalf("LoadRaw: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("defaults should validate: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("LoadRaw should not write the config file (stat err=%v)", err)
	}
}

func TestLoadRaw_UnparseableFileFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	_ = os.WriteFile(path, []byte("immich: [unclosed\n"), 0644)
	if _, err := config.LoadRaw(path); err == nil {
		t.Fatal("expected parse error")
	}
}
