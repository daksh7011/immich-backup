// internal/backup/media_test.go
package backup_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daksh7011/immich-backup/internal/backup"
)

// fakeRcloneEnv, when set, turns the test binary into a fake rclone:
// "<exitCode>:<errorLines>" — it prints errorLines JSON error-level lines to
// stderr and exits with exitCode, or kills itself when exitCode is
// killedExitCode. Lets RunMedia's exit handling be tested without a real
// rclone binary.
const fakeRcloneEnv = "IMMICH_BACKUP_FAKE_RCLONE"

// fakeRcloneArgsEnv, when set, names a file the fake rclone appends its
// argv to, one invocation per line.
const fakeRcloneArgsEnv = "IMMICH_BACKUP_FAKE_RCLONE_ARGS"

// killedExitCode makes the fake rclone kill itself, as the OOM killer would.
const killedExitCode = -9

func TestMain(m *testing.M) {
	if spec := os.Getenv(fakeRcloneEnv); spec != "" {
		os.Exit(runFakeRclone(spec))
	}
	os.Exit(m.Run())
}

func runFakeRclone(spec string) int {
	codeStr, nStr, _ := strings.Cut(spec, ":")
	code, _ := strconv.Atoi(codeStr)
	n, _ := strconv.Atoi(nStr)
	if path := os.Getenv(fakeRcloneArgsEnv); path != "" {
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); err == nil {
			fmt.Fprintln(f, strings.Join(os.Args[1:], " "))
			_ = f.Close()
		}
	}
	fmt.Fprintln(os.Stderr, `{"level":"info","msg":"starting"}`)
	for i := range n {
		fmt.Fprintf(os.Stderr, `{"level":"error","msg":"file%d.jpg: read failed"}`+"\n", i)
	}
	if code == killedExitCode {
		if p, err := os.FindProcess(os.Getpid()); err == nil {
			_ = p.Kill()
		}
		time.Sleep(time.Minute)
	}
	return code
}

// useFakeRclone points the backup package at the test binary acting as rclone
// with the given behaviour.
func useFakeRclone(t *testing.T, exitCode, errorLines int) {
	t.Helper()
	t.Setenv(fakeRcloneEnv, fmt.Sprintf("%d:%d", exitCode, errorLines))
	t.Cleanup(backup.SetRcloneBin(os.Args[0]))
}

// mediaDir returns a non-empty directory usable as upload_location.
func mediaDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "photo.jpg"), []byte("x"), 0644); err != nil {
		t.Fatalf("write media file: %v", err)
	}
	return dir
}

var testOpts = backup.MediaOpts{Transfers: 2, Checkers: 4, BufferSize: "16M"}

func TestMediaSyncArgs_ExcludesDBDir(t *testing.T) {
	args := backup.MediaSyncArgs("/conf", "/src", "b2:bucket", testOpts)

	i := slices.Index(args, "--exclude")
	if i < 0 || i+1 >= len(args) || args[i+1] != "/db/**" {
		t.Fatalf("expected --exclude /db/** in args, got %v", args)
	}
	if slices.Contains(args, "--delete-excluded") {
		t.Errorf("--delete-excluded would delete the uploaded DB dumps: %v", args)
	}
	sync := slices.Index(args, "sync")
	if sync < 0 || sync+2 >= len(args) || args[sync+1] != "/src" || args[sync+2] != "b2:bucket" {
		t.Errorf("expected sync /src b2:bucket, got %v", args)
	}
	c := slices.Index(args, "--config")
	if c < 0 || args[c+1] != "/conf" {
		t.Errorf("expected --config /conf, got %v", args)
	}
}

func TestCheckUploadLocation(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		dir     string
		wantErr string
	}{
		{"missing", filepath.Join(t.TempDir(), "nope"), "does not exist"},
		{"not a dir", file, "not a directory"},
		{"empty", t.TempDir(), "is empty"},
		{"unset", "", "not set"},
		{"ok", mediaDir(t), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := backup.CheckUploadLocation(tt.dir)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestRunMedia_NonZeroExitWithFileErrors_IsPartial(t *testing.T) {
	useFakeRclone(t, 1, 2)
	r := backup.New(nil, "unused.conf", nil)

	err := r.RunMedia(context.Background(), "dst:", mediaDir(t), testOpts, nil)
	var pe *backup.PartialError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *PartialError, got %T: %v", err, err)
	}
	if pe.FileErrors != 2 {
		t.Errorf("FileErrors: got %d, want 2", pe.FileErrors)
	}
}

func TestRunMedia_NonZeroExitWithoutFileErrors_IsError(t *testing.T) {
	useFakeRclone(t, 1, 0)
	r := backup.New(nil, "unused.conf", nil)

	err := r.RunMedia(context.Background(), "dst:", mediaDir(t), testOpts, nil)
	if err == nil {
		t.Fatal("expected error for non-zero rclone exit")
	}
	var pe *backup.PartialError
	if errors.As(err, &pe) {
		t.Errorf("fatal exit must not be reported as partial: %v", err)
	}
}

func TestRunMedia_FatalExitCode_IsErrorEvenWithFileErrors(t *testing.T) {
	useFakeRclone(t, 7, 3) // rclone exit 7 = fatal (e.g. account suspended)
	r := backup.New(nil, "unused.conf", nil)

	err := r.RunMedia(context.Background(), "dst:", mediaDir(t), testOpts, nil)
	if err == nil {
		t.Fatal("expected error for fatal rclone exit")
	}
	var pe *backup.PartialError
	if errors.As(err, &pe) {
		t.Errorf("fatal exit must not be reported as partial: %v", err)
	}
}

func TestRunMedia_ZeroExitAfterRetriedErrors_IsSuccess(t *testing.T) {
	// rclone logs "Attempt 1/3 failed" at error level, then succeeds on retry.
	useFakeRclone(t, 0, 1)
	r := backup.New(nil, "unused.conf", nil)

	if err := r.RunMedia(context.Background(), "dst:", mediaDir(t), testOpts, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_EmptyUploadLocation_FailsBeforeSync(t *testing.T) {
	// Fake rclone would "succeed"; the preflight must stop the run first.
	useFakeRclone(t, 0, 0)

	ch := make(chan any, 20)
	go backup.Run(context.Background(), "unused.conf", "", "", t.TempDir(), "dst:", nil,
		true, false, testOpts, nil, ch)
	msgs := collectChan(ch)

	last := msgs[len(msgs)-1]
	em, ok := last.(backup.ErrorMsg)
	if !ok {
		t.Fatalf("expected ErrorMsg as last message, got %T: %v", last, last)
	}
	if !strings.Contains(em.Err.Error(), "is empty") {
		t.Errorf("error should explain the empty upload location: %v", em.Err)
	}
}

func TestRun_PartialMediaSync_SendsPartialError(t *testing.T) {
	useFakeRclone(t, 1, 1)

	ch := make(chan any, 20)
	go backup.Run(context.Background(), "unused.conf", "", "", mediaDir(t), "dst:", nil,
		true, false, testOpts, nil, ch)
	msgs := collectChan(ch)

	last := msgs[len(msgs)-1]
	em, ok := last.(backup.ErrorMsg)
	if !ok {
		t.Fatalf("expected ErrorMsg as last message, got %T: %v", last, last)
	}
	var pe *backup.PartialError
	if !errors.As(em.Err, &pe) {
		t.Errorf("expected wrapped *PartialError, got %v", em.Err)
	}
}
