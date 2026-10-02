package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/docker"
	"github.com/daksh7011/immich-backup/internal/doctor"
	"github.com/daksh7011/immich-backup/internal/status"
)

// fakeClient satisfies backupClient without a Docker daemon.
type fakeClient struct{ closed bool }

func (f *fakeClient) Exec(string, string, ...string) ([]byte, error) { return nil, nil }
func (f *fakeClient) ExecStream(context.Context, io.Writer, string, string, ...string) error {
	return nil
}
func (f *fakeClient) IsContainerRunning(string) (bool, error) { return true, nil }
func (f *fakeClient) Close()                                  { f.closed = true }

func testBackupDeps(t *testing.T, client *fakeClient, results []doctor.CheckResult) backupDeps {
	t.Helper()
	return backupDeps{
		newClient: func() (backupClient, error) { return client, nil },
		check: func(docker.Executor, *config.Config, string) []doctor.CheckResult {
			return results
		},
		statusPath:     filepath.Join(t.TempDir(), "last-run.json"),
		rcloneConfPath: filepath.Join(t.TempDir(), "rclone.conf"),
	}
}

func testBackupConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := &config.Config{}
	cfg.Daemon.LogPath = filepath.Join(t.TempDir(), "logs", "daemon.log")
	return cfg
}

func loadStatus(t *testing.T, path string) *status.LastRun {
	t.Helper()
	run, err := status.Load(path)
	if err != nil {
		t.Fatalf("expected a status record at %s: %v", path, err)
	}
	return run
}

func TestRunBackup_DoctorFailureRecordsStatus(t *testing.T) {
	client := &fakeClient{}
	deps := testBackupDeps(t, client, []doctor.CheckResult{
		{Name: "rclone Binary", OK: false, Message: "rclone not found", Remedy: "install rclone"},
		{Name: "Docker Socket", OK: true, Message: "ok"},
		{Name: "Postgres Container", OK: false, Message: "immich_postgres is not running"},
	})

	err := runBackup(newBackupCmd(), testBackupConfig(t), deps)
	if err == nil {
		t.Fatal("expected an error so the process exits non-zero")
	}
	for _, want := range []string{"rclone Binary: rclone not found", "Postgres Container: immich_postgres is not running"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "Docker Socket") {
		t.Errorf("error %q names a passing check", err)
	}
	if !client.closed {
		t.Error("docker client was not closed")
	}

	run := loadStatus(t, deps.statusPath)
	if run.Result != status.ResultError {
		t.Errorf("Result: got %q, want %q", run.Result, status.ResultError)
	}
	if run.Error != err.Error() {
		t.Errorf("Error: got %q, want %q", run.Error, err.Error())
	}
	if run.Time.IsZero() {
		t.Error("Time not set")
	}
}

func TestRunBackup_DockerClientFailureRecordsStatus(t *testing.T) {
	deps := testBackupDeps(t, nil, nil)
	deps.newClient = func() (backupClient, error) { return nil, errors.New("bad DOCKER_HOST") }

	err := runBackup(newBackupCmd(), testBackupConfig(t), deps)
	if err == nil || !strings.Contains(err.Error(), "bad DOCKER_HOST") {
		t.Fatalf("expected docker client error, got %v", err)
	}
	run := loadStatus(t, deps.statusPath)
	if run.Result != status.ResultError || !strings.Contains(run.Error, "bad DOCKER_HOST") {
		t.Errorf("status: got %+v", run)
	}
}

func TestRunBackup_LogFileFailureRecordsStatus(t *testing.T) {
	deps := testBackupDeps(t, &fakeClient{}, nil)
	deps.newClient = func() (backupClient, error) {
		t.Fatal("docker must not be contacted when the log file cannot be created")
		return nil, nil
	}
	parent := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(parent, nil, 0644); err != nil {
		t.Fatal(err)
	}
	cfg := testBackupConfig(t)
	cfg.Daemon.LogPath = filepath.Join(parent, "daemon.log")

	err := runBackup(newBackupCmd(), cfg, deps)
	if err == nil {
		t.Fatal("expected an error")
	}
	run := loadStatus(t, deps.statusPath)
	if run.Result != status.ResultError || !strings.Contains(run.Error, "daemon log") {
		t.Errorf("status: got %+v", run)
	}
}

func TestRunBackup_FailureKeepsLastSuccess(t *testing.T) {
	deps := testBackupDeps(t, &fakeClient{}, []doctor.CheckResult{{Name: "Config", OK: false, Message: "bad"}})
	prev := &status.LastRun{Time: time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC), Result: status.ResultSuccess}
	if err := status.Record(deps.statusPath, prev); err != nil {
		t.Fatal(err)
	}
	if err := runBackup(newBackupCmd(), testBackupConfig(t), deps); err == nil {
		t.Fatal("expected an error")
	}
	run := loadStatus(t, deps.statusPath)
	if run.Result != status.ResultError || !run.LastSuccess.Equal(prev.Time) {
		t.Errorf("status: got %+v, want LastSuccess %v", run, prev.Time)
	}
}

func TestRunBackup_UsageErrorDoesNotRecordStatus(t *testing.T) {
	deps := testBackupDeps(t, &fakeClient{}, nil)
	c := newBackupCmd()
	_ = c.Flags().Set("skip-db", "true")
	_ = c.Flags().Set("skip-media", "true")

	if err := runBackup(c, testBackupConfig(t), deps); err == nil {
		t.Fatal("expected a usage error")
	}
	if _, err := os.Stat(deps.statusPath); !os.IsNotExist(err) {
		t.Errorf("usage error must not write a status record (stat err: %v)", err)
	}
}

func TestLoadCommandConfig_BackupFailureRecordsStatus(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	statusPath := filepath.Join(dir, "last-run.json")
	if err := os.WriteFile(cfgPath, []byte("daemon: [not, a, map]\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadCommandConfig("immich-backup backup", cfgPath, statusPath)
	if err == nil {
		t.Fatal("expected a config error")
	}
	run := loadStatus(t, statusPath)
	if run.Result != status.ResultError || !strings.Contains(run.Error, "config") {
		t.Errorf("status: got %+v", run)
	}
}

func TestLoadCommandConfig_OtherCommandDoesNotRecordStatus(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	statusPath := filepath.Join(dir, "last-run.json")
	if err := os.WriteFile(cfgPath, []byte("daemon: [not, a, map]\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := loadCommandConfig("immich-backup status", cfgPath, statusPath); err == nil {
		t.Fatal("expected a config error")
	}
	if _, err := os.Stat(statusPath); !os.IsNotExist(err) {
		t.Errorf("status must only record backup attempts (stat err: %v)", err)
	}
}

func TestMaybePrintBanner(t *testing.T) {
	var buf bytes.Buffer
	maybePrintBanner(&buf, func() bool { return false })
	if buf.Len() != 0 {
		t.Errorf("banner printed when stdout is not a terminal:\n%s", buf.String())
	}

	maybePrintBanner(&buf, func() bool { return true })
	if !strings.Contains(buf.String(), "immich-backup") {
		t.Errorf("banner missing on a terminal, got:\n%s", buf.String())
	}
}
