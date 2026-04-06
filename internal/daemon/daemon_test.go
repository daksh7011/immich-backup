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

func TestGeneratePlist_ContainsLabel(t *testing.T) {
	plist := daemon.GeneratePlist("/usr/local/bin/immich-backup", testCfg)
	if !strings.Contains(plist, "com.immich-backup.agent") {
		t.Errorf("plist missing label: %s", plist)
	}
}

func TestGeneratePlist_ContainsBinaryPath(t *testing.T) {
	plist := daemon.GeneratePlist("/usr/local/bin/immich-backup", testCfg)
	if !strings.Contains(plist, "/usr/local/bin/immich-backup") {
		t.Errorf("plist missing binary path: %s", plist)
	}
}

func TestGeneratePlist_ContainsLogPath(t *testing.T) {
	plist := daemon.GeneratePlist("/usr/local/bin/immich-backup", testCfg)
	if !strings.Contains(plist, testCfg.Daemon.LogPath) {
		t.Errorf("plist missing log path: %s", plist)
	}
}

func TestGeneratePlist_NoRootPaths(t *testing.T) {
	plist := daemon.GeneratePlist("/usr/local/bin/immich-backup", testCfg)
	if strings.Contains(plist, "/Library/LaunchDaemons") {
		t.Error("plist must not reference root LaunchDaemons path")
	}
}

func TestGenerateSystemdUnit_ContainsExecStart(t *testing.T) {
	unit := daemon.GenerateSystemdUnit("/usr/local/bin/immich-backup", testCfg)
	if !strings.Contains(unit, "ExecStart=/usr/local/bin/immich-backup") {
		t.Errorf("unit missing ExecStart: %s", unit)
	}
}

func TestGenerateSystemdUnit_ContainsWantedBy(t *testing.T) {
	unit := daemon.GenerateSystemdUnit("/usr/local/bin/immich-backup", testCfg)
	if !strings.Contains(unit, "WantedBy=default.target") {
		t.Errorf("unit missing WantedBy: %s", unit)
	}
}
