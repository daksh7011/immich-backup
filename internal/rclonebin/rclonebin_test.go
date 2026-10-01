// internal/rclonebin/rclonebin_test.go
package rclonebin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFakeRclone creates an executable file named rclone in dir.
func writeFakeRclone(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, binName)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func notFound(string) (string, error) { return "", errors.New("not found") }

func TestResolve_FindsBinaryInHomeFallbackWhenPATHEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PATH", "")
	want := writeFakeRclone(t, filepath.Join(home, ".local", "bin"))

	got, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != want {
		t.Errorf("Resolve() = %q, want %q", got, want)
	}
}

func TestResolve_PrefersLookPath(t *testing.T) {
	fallback := t.TempDir()
	writeFakeRclone(t, fallback)
	lookPath := func(string) (string, error) { return "/usr/bin/rclone", nil }

	got, err := resolve(lookPath, []string{fallback})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != mustAbs(t, "/usr/bin/rclone") {
		t.Errorf("resolve() = %q, want the LookPath result", got)
	}
}

func TestResolve_FallsBackInOrder(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	want := writeFakeRclone(t, second)

	got, err := resolve(notFound, []string{first, second})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != want {
		t.Errorf("resolve() = %q, want %q", got, want)
	}
}

func TestResolve_SkipsDirectoryNamedRclone(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, binName), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve(notFound, []string{dir}); err == nil {
		t.Error("expected error when only a directory named rclone exists")
	}
}

func TestResolve_NotFoundNamesSearchedDirs(t *testing.T) {
	dirs := []string{t.TempDir(), t.TempDir()}
	_, err := resolve(notFound, dirs)
	if err == nil {
		t.Fatal("expected error when rclone is nowhere")
	}
	for _, d := range dirs {
		if !strings.Contains(err.Error(), d) {
			t.Errorf("error %q does not name searched dir %q", err, d)
		}
	}
}

func TestPath_FallsBackToBareNameWhenUnresolved(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PATH", "")
	if _, err := Resolve(); err == nil {
		t.Skip("rclone present in a system fallback dir on this machine")
	}
	if got := Path(); got != binName {
		t.Errorf("Path() = %q, want %q", got, binName)
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}
