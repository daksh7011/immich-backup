// internal/backup/dump_test.go
package backup_test

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/daksh7011/immich-backup/internal/backup"
)

// fakeExecutor streams a canned pg_dumpall output, fails, or blocks until
// the context is cancelled, without Docker.
type fakeExecutor struct {
	out   string
	err   error
	block bool
}

func (f fakeExecutor) Exec(string, string, ...string) ([]byte, error) { return []byte(f.out), f.err }

func (f fakeExecutor) ExecStream(ctx context.Context, w io.Writer, _, _ string, _ ...string) error {
	if f.block {
		<-ctx.Done()
		return ctx.Err()
	}
	if _, err := io.WriteString(w, f.out); err != nil {
		return err
	}
	return f.err
}

func (fakeExecutor) IsContainerRunning(string) (bool, error) { return true, nil }

// privateTempDir points os.TempDir at a fresh directory for the test.
func privateTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, k := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(k, dir)
	}
	return dir
}

func assertEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	if len(entries) != 0 {
		t.Errorf("temp files left behind in %s: %v", dir, entries)
	}
}

func TestDBRemotePath(t *testing.T) {
	cases := map[string]string{
		"nas:":             "nas:db",
		"nas:/":            "nas:/db",
		"nas:backup":       "nas:backup/db",
		"nas:backup/":      "nas:backup/db",
		"b2:bucket/immich": "b2:bucket/immich/db",
		"/srv/backup/":     "/srv/backup/db",
	}
	for remote, want := range cases {
		if got := backup.DBRemotePath(remote); got != want {
			t.Errorf("DBRemotePath(%q) = %q, want %q", remote, got, want)
		}
	}
}

func TestMediaSyncArgs_LogLevelInfo(t *testing.T) {
	args := backup.MediaSyncArgs("/conf", "/src", "b2:bucket", testOpts)
	i := slices.Index(args, "--log-level")
	if i < 0 || args[i+1] != "INFO" {
		t.Errorf("expected --log-level INFO (DEBUG logs every unchanged file), got %v", args)
	}
}

func TestRunDatabase_StreamsGzipToPrivateFile(t *testing.T) {
	r := backup.New(fakeExecutor{out: "-- PostgreSQL database dump\n"}, "unused.conf", nil)
	dest := filepath.Join(t.TempDir(), "dump.sql.gz")
	if err := r.RunDatabase(context.Background(), "pg", "postgres", dest); err != nil {
		t.Fatalf("RunDatabase: %v", err)
	}
	f, err := os.Open(dest)
	if err != nil {
		t.Fatalf("open dump: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("not a valid gzip: %v", err)
	}
	content, _ := io.ReadAll(gz)
	if string(content) != "-- PostgreSQL database dump\n" {
		t.Errorf("dump content: %q", content)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(dest)
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Errorf("dump mode %o, want 0600 (it holds password hashes)", perm)
		}
	}
}

func TestRunDatabase_FailureRemovesDump(t *testing.T) {
	r := backup.New(fakeExecutor{out: "partial", err: errors.New("exec exited with code 1")}, "unused.conf", nil)
	dir := t.TempDir()
	if err := r.RunDatabase(context.Background(), "pg", "postgres", filepath.Join(dir, "dump.sql.gz")); err == nil {
		t.Fatal("expected error from a failed pg_dumpall")
	}
	assertEmptyDir(t, dir)
}

func TestRun_DumpUploadedToDBDirAndRemoved(t *testing.T) {
	tmp := privateTempDir(t)
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv(fakeRcloneArgsEnv, argsFile)
	useFakeRclone(t, 0, 0)

	ch := make(chan any, 20)
	go backup.Run(context.Background(), "unused.conf", "pg", "postgres", mediaDir(t), "nas:",
		fakeExecutor{out: "dump"}, false, false, testOpts, nil, ch)
	msgs := collectChan(ch)
	if _, ok := msgs[len(msgs)-1].(backup.DoneMsg); !ok {
		t.Fatalf("expected DoneMsg, got %v", msgs)
	}

	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read fake rclone args: %v", err)
	}
	calls := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(calls) != 2 {
		t.Fatalf("expected a copy and a sync, got %q", calls)
	}
	if !strings.Contains(calls[0], "copy ") || !strings.Contains(calls[0], " nas:db ") {
		t.Errorf("dump should go to nas:db (next to the media), got %q", calls[0])
	}
	if !strings.Contains(calls[1], "sync ") || !strings.Contains(calls[1], " nas: ") {
		t.Errorf("media should sync to nas:, got %q", calls[1])
	}
	assertEmptyDir(t, tmp)
}

func TestRun_CancelledDuringDump_ClosesWithoutErrorAndCleansUp(t *testing.T) {
	tmp := privateTempDir(t)
	ctx, cancel := context.WithCancel(context.Background())

	ch := make(chan any, 20)
	go backup.Run(ctx, "unused.conf", "pg", "postgres", mediaDir(t), "nas:",
		fakeExecutor{block: true}, false, false, testOpts, nil, ch)
	if msg := <-ch; msg != (backup.PhaseMsg{Phase: backup.PhaseDBDump}) {
		t.Fatalf("expected the dump phase first, got %v", msg)
	}
	cancel()
	for msg := range ch {
		switch msg.(type) {
		case backup.ErrorMsg, backup.DoneMsg:
			t.Errorf("a cancelled run must close without a terminal message, got %#v", msg)
		}
	}
	assertEmptyDir(t, tmp)
}

func TestRunMedia_TransferLimitExit_IsError(t *testing.T) {
	useFakeRclone(t, 8, 2) // rclone exit 8 = --max-transfer reached
	r := backup.New(nil, "unused.conf", nil)

	err := r.RunMedia(context.Background(), "dst:", mediaDir(t), testOpts, nil)
	var pe *backup.PartialError
	if err == nil || errors.As(err, &pe) {
		t.Fatalf("exit 8 is not a file-level error, got %v", err)
	}
}

func TestRunMedia_KilledBySignal_IsErrorNotPartial(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no signals: a killed process exits with code 1")
	}
	useFakeRclone(t, killedExitCode, 1)
	r := backup.New(nil, "unused.conf", nil)

	err := r.RunMedia(context.Background(), "dst:", mediaDir(t), testOpts, nil)
	if err == nil {
		t.Fatal("expected error when rclone is killed")
	}
	var pe *backup.PartialError
	if errors.As(err, &pe) {
		t.Errorf("a killed rclone never finished; must not be partial: %v", err)
	}
}
