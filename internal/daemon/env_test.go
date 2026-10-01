// internal/daemon/env_test.go
package daemon_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/daksh7011/immich-backup/internal/daemon"
)

var testEnv = daemon.ServiceEnv{Path: "/home/user/.local/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"}

func TestBuildServicePATH_PutsRcloneDirFirst(t *testing.T) {
	got := daemon.BuildServicePATH("/home/linuxbrew/.linuxbrew/bin")
	if !strings.HasPrefix(got, "/home/linuxbrew/.linuxbrew/bin:") {
		t.Errorf("PATH should start with the rclone dir, got %q", got)
	}
	for _, d := range []string{"/usr/local/bin", "/usr/bin", "/bin"} {
		if !strings.Contains(":"+got+":", ":"+d+":") {
			t.Errorf("PATH %q missing standard dir %q", got, d)
		}
	}
}

func TestBuildServicePATH_Deduplicates(t *testing.T) {
	got := daemon.BuildServicePATH("/usr/bin")
	if n := strings.Count(":"+got+":", ":/usr/bin:"); n != 1 {
		t.Errorf("/usr/bin appears %d times in %q, want 1", n, got)
	}
	if !strings.HasPrefix(got, "/usr/bin:") {
		t.Errorf("rclone dir should stay first, got %q", got)
	}
}

func TestResolveServiceEnv_UsesRcloneDirAndDockerHost(t *testing.T) {
	resolve := func() (string, error) { return "/opt/homebrew/bin/rclone", nil }
	getenv := func(k string) string {
		if k == "DOCKER_HOST" {
			return "unix:///run/user/1000/docker.sock"
		}
		return ""
	}
	env, err := daemon.ResolveServiceEnvWith(resolve, getenv)
	if err != nil {
		t.Fatalf("ResolveServiceEnv: %v", err)
	}
	if !strings.HasPrefix(env.Path, "/opt/homebrew/bin:") {
		t.Errorf("PATH should start with rclone's dir, got %q", env.Path)
	}
	if env.DockerHost != "unix:///run/user/1000/docker.sock" {
		t.Errorf("DockerHost = %q", env.DockerHost)
	}
}

func TestResolveServiceEnv_FailsLoudlyWithoutRclone(t *testing.T) {
	resolve := func() (string, error) { return "", errors.New("rclone not found in PATH or /usr/bin") }
	_, err := daemon.ResolveServiceEnvWith(resolve, func(string) string { return "" })
	if err == nil {
		t.Fatal("expected error when rclone cannot be resolved")
	}
	if !strings.Contains(err.Error(), "rclone") || !strings.Contains(err.Error(), "rclone.org/install") {
		t.Errorf("error should name rclone and the install remedy, got %q", err)
	}
}

func TestGenerateSystemdUnit_ContainsQuotedPATH(t *testing.T) {
	unit := daemon.GenerateSystemdUnit("/usr/local/bin/immich-backup", testCfg, testEnv)
	want := `Environment="PATH=` + testEnv.Path + `"`
	if !strings.Contains(unit, want) {
		t.Errorf("unit missing %s:\n%s", want, unit)
	}
	if strings.Contains(unit, "DOCKER_HOST") {
		t.Errorf("unit must not set DOCKER_HOST when it is empty:\n%s", unit)
	}
}

func TestGenerateSystemdUnit_ContainsDockerHostWhenSet(t *testing.T) {
	env := testEnv
	env.DockerHost = "unix:///run/user/1000/docker.sock"
	unit := daemon.GenerateSystemdUnit("/usr/local/bin/immich-backup", testCfg, env)
	if !strings.Contains(unit, `Environment="DOCKER_HOST=unix:///run/user/1000/docker.sock"`) {
		t.Errorf("unit missing DOCKER_HOST:\n%s", unit)
	}
}

func TestGenerateSystemdUnit_EnvironmentIsInServiceSection(t *testing.T) {
	unit := daemon.GenerateSystemdUnit("/usr/local/bin/immich-backup", testCfg, testEnv)
	svc := strings.Index(unit, "[Service]")
	envAt := strings.Index(unit, "Environment=")
	if svc < 0 || envAt < svc {
		t.Errorf("Environment= must be inside [Service]:\n%s", unit)
	}
}

func TestGenerateSystemdUnit_EscapesEnvironmentValues(t *testing.T) {
	env := daemon.ServiceEnv{
		Path:       `/opt/100%/bin:/usr/bin`,
		DockerHost: `tcp://h"o\st:2375`,
	}
	unit := daemon.GenerateSystemdUnit("/usr/local/bin/immich-backup", testCfg, env)
	if !strings.Contains(unit, `Environment="PATH=/opt/100%%/bin:/usr/bin"`) {
		t.Errorf("%% must be escaped as %%%% in PATH:\n%s", unit)
	}
	if !strings.Contains(unit, `Environment="DOCKER_HOST=tcp://h\"o\\st:2375"`) {
		t.Errorf("quote and backslash must be escaped in DOCKER_HOST:\n%s", unit)
	}
}

func TestGenerateSystemdUnit_EscapesNewlineInValue(t *testing.T) {
	env := daemon.ServiceEnv{Path: "/usr/bin", DockerHost: "unix:///a\n[Service]"}
	unit := daemon.GenerateSystemdUnit("/usr/local/bin/immich-backup", testCfg, env)
	if strings.Count(unit, "\n[Service]") != 1 {
		t.Errorf("newline in a value must not start a new line:\n%s", unit)
	}
	if !strings.Contains(unit, `DOCKER_HOST=unix:///a\n[Service]"`) {
		t.Errorf("newline should be C-escaped:\n%s", unit)
	}
}

func TestGeneratePlist_ContainsEnvironmentVariables(t *testing.T) {
	plist, err := daemon.GeneratePlist("/usr/local/bin/immich-backup", testCfg, testEnv)
	if err != nil {
		t.Fatalf("GeneratePlist: %v", err)
	}
	if !strings.Contains(plist, "<key>EnvironmentVariables</key>") {
		t.Errorf("plist missing EnvironmentVariables:\n%s", plist)
	}
	if !strings.Contains(plist, "<key>PATH</key>") || !strings.Contains(plist, "<string>"+testEnv.Path+"</string>") {
		t.Errorf("plist missing PATH entry:\n%s", plist)
	}
	if strings.Contains(plist, "DOCKER_HOST") {
		t.Errorf("plist must not set DOCKER_HOST when it is empty:\n%s", plist)
	}
}

func TestGeneratePlist_ContainsDockerHostWhenSet(t *testing.T) {
	env := testEnv
	env.DockerHost = "unix:///Users/me/.colima/default/docker.sock"
	plist, err := daemon.GeneratePlist("/usr/local/bin/immich-backup", testCfg, env)
	if err != nil {
		t.Fatalf("GeneratePlist: %v", err)
	}
	if !strings.Contains(plist, "<key>DOCKER_HOST</key>") ||
		!strings.Contains(plist, "<string>unix:///Users/me/.colima/default/docker.sock</string>") {
		t.Errorf("plist missing DOCKER_HOST entry:\n%s", plist)
	}
}

func TestGeneratePlist_EscapesEnvironmentValues(t *testing.T) {
	env := daemon.ServiceEnv{Path: "/opt/a&b/<bin>:/usr/bin"}
	plist, err := daemon.GeneratePlist("/usr/local/bin/immich-backup", testCfg, env)
	if err != nil {
		t.Fatalf("GeneratePlist: %v", err)
	}
	if !strings.Contains(plist, "<string>/opt/a&amp;b/&lt;bin&gt;:/usr/bin</string>") {
		t.Errorf("PATH must be XML-escaped:\n%s", plist)
	}
}
