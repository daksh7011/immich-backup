// internal/daemon/logfile_test.go
package daemon_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/daksh7011/immich-backup/internal/daemon"
)

func TestEnsureLogFile_CreatesNestedDirsAndFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "logs", "daemon.log")
	if err := daemon.EnsureLogFile(path); err != nil {
		t.Fatalf("EnsureLogFile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("log file not created: %v", err)
	}
	if !info.Mode().IsRegular() {
		t.Errorf("expected regular file, got mode %v", info.Mode())
	}
}

func TestEnsureLogFile_KeepsExistingContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.log")
	if err := os.WriteFile(path, []byte("previous run\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := daemon.EnsureLogFile(path); err != nil {
		t.Fatalf("EnsureLogFile: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "previous run\n" {
		t.Errorf("existing log content changed: %q", data)
	}
}

func TestEnsureLogFile_RejectsRelativePath(t *testing.T) {
	if err := daemon.EnsureLogFile(filepath.Join("logs", "daemon.log")); err == nil {
		t.Error("expected error for relative log path, got nil")
	}
}

func TestEnsureLogFile_RejectsEmptyPath(t *testing.T) {
	if err := daemon.EnsureLogFile(""); err == nil {
		t.Error("expected error for empty log path, got nil")
	}
}

func TestEnsureLogFile_FailsWhenParentIsFile(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "logs")
	if err := os.WriteFile(blocker, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := daemon.EnsureLogFile(filepath.Join(blocker, "daemon.log")); err == nil {
		t.Error("expected error when log dir path is a file, got nil")
	}
}
