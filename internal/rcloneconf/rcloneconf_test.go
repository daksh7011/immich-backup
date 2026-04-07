// internal/rcloneconf/rcloneconf_test.go
package rcloneconf_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/daksh7011/immich-backup/internal/rcloneconf"
)

const validConf = "[test-local]\ntype = local\n"

func TestEnsureConfigured_AlreadyConfigured(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rclone.conf")
	if err := os.WriteFile(path, []byte(validConf), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := rcloneconf.EnsureConfigured(path); err != nil {
		t.Fatalf("unexpected error for configured remote: %v", err)
	}
}

func TestEnsureConfigured_EmptyFile(t *testing.T) {
	// EnsureConfigured is now a pure validator: it must return an error
	// immediately when the config has no remotes, without launching rclone.
	path := filepath.Join(t.TempDir(), "rclone.conf")
	if err := os.WriteFile(path, []byte(""), 0600); err != nil {
		t.Fatalf("write empty config: %v", err)
	}
	if err := rcloneconf.EnsureConfigured(path); err == nil {
		t.Fatal("expected error for empty config, got nil")
	}
}
