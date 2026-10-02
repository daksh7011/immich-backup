// internal/daemon/executable_test.go
package daemon_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/daksh7011/immich-backup/internal/daemon"
)

const cellarExe = "/home/linuxbrew/.linuxbrew/Cellar/immich-backup/0.1.8/bin/immich-backup"

func noLookPath(string) (string, error) { return "", errors.New("not found") }

// sameAs returns a sameFile func that reports true only for the given pairs
// of paths (in either order), standing in for os.Stat + os.SameFile.
func sameAs(pairs ...[2]string) func(a, b string) bool {
	return func(a, b string) bool {
		for _, p := range pairs {
			if (a == p[0] && b == p[1]) || (a == p[1] && b == p[0]) {
				return true
			}
		}
		return false
	}
}

func TestStableExecutable_PrefersLookPathWhenSameFile(t *testing.T) {
	link := "/home/linuxbrew/.linuxbrew/bin/immich-backup"
	lookPath := func(name string) (string, error) {
		if name != "immich-backup" {
			t.Errorf("LookPath(%q), want immich-backup", name)
		}
		return link, nil
	}
	got, err := daemon.StableExecutableWith(cellarExe, lookPath, sameAs([2]string{link, cellarExe}), "/tmp")
	if err != nil {
		t.Fatalf("StableExecutable: %v", err)
	}
	if got != link {
		t.Errorf("got %q, want the PATH entry %q", got, link)
	}
}

func TestStableExecutable_IgnoresLookPathForDifferentFile(t *testing.T) {
	exe := "/usr/local/bin/immich-backup"
	lookPath := func(string) (string, error) { return "/home/u/go/bin/immich-backup", nil }
	got, err := daemon.StableExecutableWith(exe, lookPath, sameAs(), "/tmp")
	if err != nil {
		t.Fatalf("StableExecutable: %v", err)
	}
	if got != exe {
		t.Errorf("got %q, want the running executable %q", got, exe)
	}
}

func TestStableExecutable_IgnoresRelativeLookPath(t *testing.T) {
	exe := "/usr/local/bin/immich-backup"
	lookPath := func(string) (string, error) { return "immich-backup", nil }
	got, err := daemon.StableExecutableWith(exe, lookPath, func(string, string) bool { return true }, "/tmp")
	if err != nil {
		t.Fatalf("StableExecutable: %v", err)
	}
	if got != exe {
		t.Errorf("got %q, want absolute %q", got, exe)
	}
}

func TestStableExecutable_MapsCellarToOptLink(t *testing.T) {
	opt := "/home/linuxbrew/.linuxbrew/opt/immich-backup/bin/immich-backup"
	got, err := daemon.StableExecutableWith(cellarExe, noLookPath, sameAs([2]string{opt, cellarExe}), "/tmp")
	if err != nil {
		t.Fatalf("StableExecutable: %v", err)
	}
	if got != opt {
		t.Errorf("got %q, want %q", got, opt)
	}
}

func TestStableExecutable_MapsCellarToBinLink(t *testing.T) {
	bin := "/opt/homebrew/bin/immich-backup"
	exe := "/opt/homebrew/Cellar/immich-backup/0.1.8/bin/immich-backup"
	got, err := daemon.StableExecutableWith(exe, noLookPath, sameAs([2]string{bin, exe}), "/tmp")
	if err != nil {
		t.Fatalf("StableExecutable: %v", err)
	}
	if got != bin {
		t.Errorf("got %q, want %q", got, bin)
	}
}

func TestStableExecutable_KeepsCellarPathWhenNoLinkMatches(t *testing.T) {
	got, err := daemon.StableExecutableWith(cellarExe, noLookPath, sameAs(), "/tmp")
	if err != nil {
		t.Fatalf("StableExecutable: %v", err)
	}
	if got != cellarExe {
		t.Errorf("got %q, want %q", got, cellarExe)
	}
}

func TestStableExecutable_RejectsGoBuildAndTempPaths(t *testing.T) {
	for _, exe := range []string{
		"/tmp/go-build123456/b001/exe/immich-backup",
		"/var/folders/xy/abc/T/go-build98765/b001/exe/immich-backup",
		"/tmp/immich-backup",
	} {
		_, err := daemon.StableExecutableWith(exe, noLookPath, sameAs(), "/tmp")
		if err == nil {
			t.Errorf("%s: expected error for a temporary binary", exe)
			continue
		}
		if !strings.Contains(err.Error(), "daemon install") {
			t.Errorf("%s: error should give the re-install remedy, got %q", exe, err)
		}
	}
}

func TestStableExecutable_TempDirPrefixIsPathAware(t *testing.T) {
	exe := "/tmpfoo/bin/immich-backup"
	got, err := daemon.StableExecutableWith(exe, noLookPath, sameAs(), "/tmp")
	if err != nil {
		t.Fatalf("StableExecutable: %v", err)
	}
	if got != exe {
		t.Errorf("got %q, want %q", got, exe)
	}
}

func TestStableExecutable_RejectsRelativePath(t *testing.T) {
	if _, err := daemon.StableExecutableWith("immich-backup", noLookPath, sameAs(), "/tmp"); err == nil {
		t.Error("expected error for a relative executable path")
	}
}

// TestStableExecutable_CellarSymlinkOnDisk builds a fake Homebrew prefix with
// a real <prefix>/bin symlink into the Cellar and uses the default sameFile.
func TestStableExecutable_CellarSymlinkOnDisk(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Homebrew layout and symlinks are Unix-only")
	}
	prefix := t.TempDir()
	kegBin := filepath.Join(prefix, "Cellar", "immich-backup", "0.1.8", "bin")
	if err := os.MkdirAll(kegBin, 0755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(kegBin, "immich-backup")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(prefix, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(prefix, "bin", "immich-backup")
	if err := os.Symlink("../Cellar/immich-backup/0.1.8/bin/immich-backup", link); err != nil {
		t.Fatal(err)
	}
	// prefix lives under the real temp dir, so pass an unrelated tmpDir.
	got, err := daemon.StableExecutableWith(exe, noLookPath, daemon.SameFile, "/nonexistent-tmp")
	if err != nil {
		t.Fatalf("StableExecutable: %v", err)
	}
	if got != link {
		t.Errorf("got %q, want the bin symlink %q", got, link)
	}
}

func TestGenerateSystemdUnit_QuotesExecStartPath(t *testing.T) {
	unit := daemon.GenerateSystemdUnit("/opt/my apps/100%/immich-backup", testCfg, testEnv)
	want := `ExecStart="/opt/my apps/100%%/immich-backup" backup`
	if !strings.Contains(unit, want) {
		t.Errorf("unit missing %s:\n%s", want, unit)
	}
}

func TestGenerateSystemdUnit_EscapesPercentInLogPath(t *testing.T) {
	cfg := *testCfg
	cfg.Daemon.LogPath = "/home/user/100%/daemon.log"
	unit := daemon.GenerateSystemdUnit("/usr/local/bin/immich-backup", &cfg, testEnv)
	if !strings.Contains(unit, "StandardOutput=append:/home/user/100%%/daemon.log") ||
		!strings.Contains(unit, "StandardError=append:/home/user/100%%/daemon.log") {
		t.Errorf("%% in log path must be escaped as %%%%:\n%s", unit)
	}
}

func TestGeneratePlist_EscapesBinaryAndLogPaths(t *testing.T) {
	cfg := *testCfg
	cfg.Daemon.LogPath = "/Users/me/a&b/<logs>/daemon.log"
	plist, err := daemon.GeneratePlist("/Users/me/R&D/<bin>/immich-backup", &cfg, testEnv)
	if err != nil {
		t.Fatalf("GeneratePlist: %v", err)
	}
	if !strings.Contains(plist, "<string>/Users/me/R&amp;D/&lt;bin&gt;/immich-backup</string>") {
		t.Errorf("binary path must be XML-escaped:\n%s", plist)
	}
	if strings.Count(plist, "<string>/Users/me/a&amp;b/&lt;logs&gt;/daemon.log</string>") != 2 {
		t.Errorf("log path must be XML-escaped in both output keys:\n%s", plist)
	}
	if strings.Contains(plist, "R&D") || strings.Contains(plist, "<bin>") || strings.Contains(plist, "<logs>") {
		t.Errorf("plist contains unescaped XML:\n%s", plist)
	}
}

func TestCheckUnitPath_RejectsControlChars(t *testing.T) {
	if err := daemon.CheckUnitPath("/home/u/logs\n[Service]/daemon.log"); err == nil {
		t.Error("expected error for a newline in a unit path")
	}
	if err := daemon.CheckUnitPath("/home/u/my logs/daemon.log"); err != nil {
		t.Errorf("spaces are fine in a unit path, got %v", err)
	}
}
