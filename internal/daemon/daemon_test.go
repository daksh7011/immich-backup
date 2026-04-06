// internal/daemon/daemon_test.go
package daemon_test

import (
	"strings"
	"testing"

	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/daemon"
)

var testCfg = &config.Config{
	Backup: config.BackupConfig{Schedule: "0 3 * * *"},
	Daemon: config.DaemonConfig{LogPath: "/home/user/.immich-backup/logs/daemon.log"},
}

func mustPlist(t *testing.T, binaryPath string, cfg *config.Config) string {
	t.Helper()
	plist, err := daemon.GeneratePlist(binaryPath, cfg)
	if err != nil {
		t.Fatalf("GeneratePlist: %v", err)
	}
	return plist
}

func TestGeneratePlist_ContainsLabel(t *testing.T) {
	plist := mustPlist(t, "/usr/local/bin/immich-backup", testCfg)
	if !strings.Contains(plist, "com.immich-backup.agent") {
		t.Errorf("plist missing label: %s", plist)
	}
}

func TestGeneratePlist_ContainsBinaryPath(t *testing.T) {
	plist := mustPlist(t, "/usr/local/bin/immich-backup", testCfg)
	if !strings.Contains(plist, "/usr/local/bin/immich-backup") {
		t.Errorf("plist missing binary path: %s", plist)
	}
}

func TestGeneratePlist_ContainsLogPath(t *testing.T) {
	plist := mustPlist(t, "/usr/local/bin/immich-backup", testCfg)
	if !strings.Contains(plist, testCfg.Daemon.LogPath) {
		t.Errorf("plist missing log path: %s", plist)
	}
}

func TestGeneratePlist_NoRootPaths(t *testing.T) {
	plist := mustPlist(t, "/usr/local/bin/immich-backup", testCfg)
	if strings.Contains(plist, "/Library/LaunchDaemons") {
		t.Error("plist must not reference root LaunchDaemons path")
	}
}

func TestGeneratePlist_StepExpressionReturnsError(t *testing.T) {
	cfg := &config.Config{
		Backup: config.BackupConfig{Schedule: "0 */4 * * *"},
		Daemon: config.DaemonConfig{LogPath: "/tmp/daemon.log"},
	}
	_, err := daemon.GeneratePlist("/usr/local/bin/immich-backup", cfg)
	if err == nil {
		t.Error("expected error for step expression in hour field, got nil")
	}
}

func TestGeneratePlist_SimpleHourMinute_ProducesIntegers(t *testing.T) {
	plist := mustPlist(t, "/usr/local/bin/immich-backup", testCfg)
	// hour=3, minute=0 must appear as plain integers in the plist XML
	if !strings.Contains(plist, "<integer>3</integer>") {
		t.Errorf("plist should contain <integer>3</integer> for hour, got:\n%s", plist)
	}
	if !strings.Contains(plist, "<integer>0</integer>") {
		t.Errorf("plist should contain <integer>0</integer> for minute, got:\n%s", plist)
	}
}

func TestGenerateSystemdUnit_ContainsExecStart(t *testing.T) {
	unit := daemon.GenerateSystemdUnit("/usr/local/bin/immich-backup", testCfg)
	if !strings.Contains(unit, "ExecStart=/usr/local/bin/immich-backup") {
		t.Errorf("unit missing ExecStart: %s", unit)
	}
}

func TestGenerateSystemdUnit_NoWantedByDefaultTarget(t *testing.T) {
	// The service file must NOT have WantedBy=default.target — the timer drives scheduling.
	unit := daemon.GenerateSystemdUnit("/usr/local/bin/immich-backup", testCfg)
	if strings.Contains(unit, "WantedBy=default.target") {
		t.Error("service unit must not have WantedBy=default.target; scheduling is driven by the timer")
	}
}

func TestGenerateSystemdTimer_SimpleSchedule(t *testing.T) {
	timer, err := daemon.GenerateSystemdTimer("0 3 * * *")
	if err != nil {
		t.Fatalf("GenerateSystemdTimer: %v", err)
	}
	if !strings.Contains(timer, "OnCalendar=*-*-* 03:00:00") {
		t.Errorf("timer missing expected OnCalendar value, got:\n%s", timer)
	}
	if !strings.Contains(timer, "WantedBy=timers.target") {
		t.Errorf("timer missing WantedBy=timers.target:\n%s", timer)
	}
}

func TestGenerateSystemdTimer_StepExpressionReturnsError(t *testing.T) {
	_, err := daemon.GenerateSystemdTimer("0 */6 * * *")
	if err == nil {
		t.Error("expected error for step expression in hour field, got nil")
	}
}
