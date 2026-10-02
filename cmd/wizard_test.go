package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daksh7011/immich-backup/internal/config"
)

func wizardConfig() *config.Config {
	return &config.Config{
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

func TestSaveWizardConfig_InvalidIsNotWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := wizardConfig()
	cfg.Daemon.LogPath = "logs/daemon.log" // the wizard does not ask for this
	err := saveWizardConfig(path, cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	for _, want := range []string{"not saved", "daemon.log_path", path} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("invalid config must not be written (stat err=%v)", statErr)
	}
}

func TestSaveWizardConfig_ValidLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := saveWizardConfig(path, wizardConfig()); err != nil {
		t.Fatalf("saveWizardConfig: %v", err)
	}
	if _, err := config.Load(path); err != nil {
		t.Errorf("saved config does not load: %v", err)
	}
}

func TestScheduleChangeNotice(t *testing.T) {
	yes := func() bool { return true }
	no := func() bool { return false }
	tests := []struct {
		name     string
		old, new string
		inst     func() bool
		want     bool
	}{
		{"unchanged", "0 3 * * *", "0 3 * * *", yes, false},
		{"whitespace only", "0 3 * * *", " 0  3 * * * ", yes, false},
		{"changed, not installed", "0 3 * * *", "0 4 * * *", no, false},
		{"changed, installed", "0 3 * * *", "0 4 * * *", yes, true},
	}
	for _, tc := range tests {
		msg := scheduleChangeNotice(tc.old, tc.new, tc.inst)
		if got := msg != ""; got != tc.want {
			t.Errorf("%s: notice = %q, want notice=%v", tc.name, msg, tc.want)
			continue
		}
		if tc.want && !strings.Contains(msg, "immich-backup daemon install") {
			t.Errorf("%s: notice %q does not tell the user to re-run daemon install", tc.name, msg)
		}
	}
}

func TestScheduleChangeNotice_UnchangedDoesNotQueryService(t *testing.T) {
	scheduleChangeNotice("0 3 * * *", "0 3 * * *", func() bool {
		t.Error("installed() called although the schedule did not change")
		return true
	})
}

func TestSetupNextSteps(t *testing.T) {
	yes := func() bool { return true }
	no := func() bool { return false }

	msg := setupNextSteps(true, no)
	for _, want := range []string{"immich-backup doctor", "immich-backup daemon install"} {
		if !strings.Contains(msg, want) {
			t.Errorf("not installed: hint %q does not mention %q", msg, want)
		}
	}
	if msg := setupNextSteps(true, yes); msg != "" {
		t.Errorf("installed: want no hint, got %q", msg)
	}
	msg = setupNextSteps(false, func() bool {
		t.Error("installed() called on a platform without a service manager")
		return false
	})
	if strings.Contains(msg, "daemon install") {
		t.Errorf("unsupported platform: hint %q must not suggest daemon install", msg)
	}
	if !strings.Contains(msg, "immich-backup doctor") {
		t.Errorf("unsupported platform: hint %q does not mention doctor", msg)
	}
}
