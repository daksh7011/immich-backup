// internal/doctor/env_check_test.go
package doctor

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type failingExecutor struct{}

func (failingExecutor) Exec(string, string, ...string) ([]byte, error) {
	return nil, errors.New("unused")
}

func (failingExecutor) IsContainerRunning(string) (bool, error) {
	return false, errors.New("dial unix /run/user/1000/docker.sock: connect: no such file or directory")
}

func TestCheckRcloneBinary_ReportsResolvedFallbackPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PATH", "")
	bin := filepath.Join(home, ".local", "bin", "rclone")
	if err := os.MkdirAll(filepath.Dir(bin), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	r := checkRcloneBinary()
	if !r.OK {
		t.Fatalf("expected rclone to be found via fallback dir, got %q", r.Message)
	}
	if !strings.Contains(r.Message, bin) {
		t.Errorf("message should name the resolved path %q, got %q", bin, r.Message)
	}
}

func TestRcloneConfResult_ReportsRcloneStderr(t *testing.T) {
	err := &exec.ExitError{Stderr: []byte("Failed to load config file: unable to decrypt configuration\n")}
	r := rcloneConfResult("/home/u/.immich-backup/rclone.conf", nil, err)
	if r.OK {
		t.Fatal("expected failure")
	}
	if !strings.Contains(r.Message, "unable to decrypt configuration") {
		t.Errorf("message should carry rclone's stderr, got %q", r.Message)
	}
}

func TestRcloneConfResult_NoRemotes(t *testing.T) {
	r := rcloneConfResult("/home/u/.immich-backup/rclone.conf", nil, nil)
	if r.OK || !strings.Contains(r.Message, "no remotes configured") {
		t.Errorf("expected no-remotes failure, got %+v", r)
	}
}

func TestRcloneConfResult_OK(t *testing.T) {
	r := rcloneConfResult("/home/u/.immich-backup/rclone.conf", []byte("b2:\n"), nil)
	if !r.OK {
		t.Errorf("expected OK, got %+v", r)
	}
}

func TestCheckDockerSocket_NamesHostAndDockerHostRemedy(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///run/user/1000/docker.sock")
	r := checkDockerSocket(failingExecutor{})
	if r.OK {
		t.Fatal("expected failure")
	}
	if !strings.Contains(r.Message, "unix:///run/user/1000/docker.sock") {
		t.Errorf("message should name the Docker host, got %q", r.Message)
	}
	if !strings.Contains(r.Remedy, "DOCKER_HOST") || !strings.Contains(r.Remedy, "daemon install") {
		t.Errorf("remedy should mention DOCKER_HOST and re-running daemon install, got %q", r.Remedy)
	}
}
