package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daksh7011/immich-backup/internal/backup"
	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/docker"
	"github.com/daksh7011/immich-backup/internal/doctor"
)

// A finished TUI waits for a key; the outcome must be recorded before that.
func TestWatchRun_RecordsOutcomeBeforeDisplayReads(t *testing.T) {
	src := make(chan any, 2)
	recorded := make(chan error, 1)
	w := watchRun(src, func(err error) { recorded <- err })

	src <- backup.DoneMsg{}
	select {
	case err := <-recorded:
		if err != nil {
			t.Errorf("recorded %v, want nil for DoneMsg", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("outcome was not recorded while the display was not reading")
	}
	if ended, err := w.outcome(); !ended || err != nil {
		t.Errorf("outcome() = %v, %v; want true, nil", ended, err)
	}
	close(src)
	w.stop(time.Second)
}

func TestFinishRun_SignalWithoutOutcomeIsAborted(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errors.New("signal: terminated"))
	src := make(chan any)
	w := watchRun(src, func(error) { t.Error("nothing should be recorded without a terminal message") })
	close(src)

	err := finishRun(ctx, func() {}, w, ctx.Err())
	if err == nil || err.Error() != "backup aborted: signal: terminated" {
		t.Errorf("got %v, want backup aborted: signal: terminated", err)
	}
}

func TestFinishRun_RunOutcomeWinsOverDisplayError(t *testing.T) {
	src := make(chan any, 1)
	w := watchRun(src, func(error) {})
	src <- backup.DoneMsg{}
	close(src)
	<-w.out // the display saw it, then failed (e.g. the terminal closed)

	if err := finishRun(context.Background(), func() {}, w, errors.New("backup TUI: read /dev/tty: EIO")); err != nil {
		t.Errorf("a finished backup is a success whatever the display did, got %v", err)
	}
}

func TestFinishRun_DisplayErrorWithoutOutcome(t *testing.T) {
	src := make(chan any)
	w := watchRun(src, func(error) {})
	go func() { time.Sleep(10 * time.Millisecond); close(src) }() // run stops after cancel

	err := finishRun(context.Background(), func() {}, w, errors.New("backup TUI: boom"))
	if err == nil || !strings.Contains(err.Error(), "backup TUI: boom") {
		t.Errorf("got %v, want the display error", err)
	}
}

func checkSequence(t *testing.T, seq ...[]doctor.CheckResult) (func(docker.Executor, *config.Config, string) []doctor.CheckResult, *int) {
	t.Helper()
	calls := 0
	return func(docker.Executor, *config.Config, string) []doctor.CheckResult {
		r := seq[min(calls, len(seq)-1)]
		calls++
		return r
	}, &calls
}

var (
	pgDown   = []doctor.CheckResult{{Name: "Docker Socket", OK: true}, {Name: "Postgres Container", Message: `container "immich_postgres" is not running`}}
	allOK    = []doctor.CheckResult{{Name: "Docker Socket", OK: true}, {Name: "Postgres Container", OK: true}}
	denied   = []doctor.CheckResult{{Name: "Docker Socket", Message: "Docker socket unreachable at unix:///var/run/docker.sock: permission denied"}}
	noRclone = []doctor.CheckResult{{Name: "rclone Binary", Message: "rclone not found"}, {Name: "Postgres Container", Message: "not running"}}
)

func TestWaitForPrerequisites_WaitsForPostgresAtBoot(t *testing.T) {
	check, calls := checkSequence(t, pgDown, pgDown, allOK)
	deps := backupDeps{check: check, prereqWait: time.Minute, prereqPoll: time.Millisecond}

	got := waitForPrerequisites(context.Background(), deps, nil, &config.Config{}, pgDown)
	if doctor.AnyFailed(got) {
		t.Errorf("expected the checks to pass once Postgres is up, got %v", got)
	}
	if *calls != 3 {
		t.Errorf("check calls: got %d, want 3", *calls)
	}
}

func TestWaitForPrerequisites_DoesNotWaitOnPermanentFailures(t *testing.T) {
	for name, initial := range map[string][]doctor.CheckResult{"permission denied": denied, "missing rclone": noRclone} {
		t.Run(name, func(t *testing.T) {
			check, calls := checkSequence(t, allOK)
			deps := backupDeps{check: check, prereqWait: time.Minute, prereqPoll: time.Millisecond}
			got := waitForPrerequisites(context.Background(), deps, nil, &config.Config{}, initial)
			if !doctor.AnyFailed(got) || *calls != 0 {
				t.Errorf("should fail at once without re-checking, calls=%d results=%v", *calls, got)
			}
		})
	}
}

func TestWaitForPrerequisites_GivesUpAfterMaxWait(t *testing.T) {
	check, _ := checkSequence(t, pgDown)
	deps := backupDeps{check: check, prereqWait: 20 * time.Millisecond, prereqPoll: time.Millisecond}
	got := waitForPrerequisites(context.Background(), deps, nil, &config.Config{}, pgDown)
	if !doctor.AnyFailed(got) {
		t.Error("expected the failure after the wait ran out")
	}
}

func TestOpenRcloneLog_RotatesLargeLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rclone.log")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, maxRcloneLogSize+1); err != nil {
		t.Fatal(err)
	}
	f := openRcloneLog(path)
	_, _ = f.Write([]byte("new\n"))
	_ = f.Close()

	if info, err := os.Stat(path + ".1"); err != nil || info.Size() != maxRcloneLogSize+1 {
		t.Errorf("expected the old log at %s.1, stat: %v", path, err)
	}
	if data, _ := os.ReadFile(path); string(data) != "new\n" {
		t.Errorf("new log should start empty, got %q", data)
	}
}

func TestOpenRcloneLog_AppendsSmallLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rclone.log")
	if err := os.WriteFile(path, []byte("old\n"), 0644); err != nil {
		t.Fatal(err)
	}
	f := openRcloneLog(path)
	_, _ = f.Write([]byte("new\n"))
	_ = f.Close()
	if data, _ := os.ReadFile(path); string(data) != "old\nnew\n" {
		t.Errorf("got %q, want the run appended", data)
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Errorf("a small log must not be rotated")
	}
}
