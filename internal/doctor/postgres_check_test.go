// internal/doctor/postgres_check_test.go
package doctor

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// pgExecutor reports the container as running or not and answers the
// pg_isready probe with readyErr, recording the command it ran.
type pgExecutor struct {
	running  bool
	readyErr error
	execCmd  []string
}

func (p *pgExecutor) Exec(_ string, command string, args ...string) ([]byte, error) {
	p.execCmd = append([]string{command}, args...)
	return nil, p.readyErr
}

func (p *pgExecutor) ExecStream(context.Context, io.Writer, string, string, ...string) error {
	return errors.New("unused")
}

func (p *pgExecutor) IsContainerRunning(string) (bool, error) { return p.running, nil }

func TestCheckPostgresContainer_ReadyWhenPgIsReadyPasses(t *testing.T) {
	ex := &pgExecutor{running: true}
	r := checkPostgresContainer(ex, "immich_postgres", "postgres")
	if !r.OK {
		t.Fatalf("expected OK, got %+v", r)
	}
	if got := strings.Join(ex.execCmd, " "); got != "pg_isready -U postgres" {
		t.Errorf("probe command: got %q, want %q", got, "pg_isready -U postgres")
	}
}

// At boot the container runs before Postgres finishes starting or recovering;
// pg_dumpall would fail then, so the check must fail (and the wait go on).
func TestCheckPostgresContainer_FailsWhilePostgresIsStarting(t *testing.T) {
	ex := &pgExecutor{running: true, readyErr: errors.New("exec exited with code 1: /var/run/postgresql:5432 - rejecting connections")}
	r := checkPostgresContainer(ex, "immich_postgres", "postgres")
	if r.OK {
		t.Fatal("expected the check to fail while Postgres rejects connections")
	}
	if r.Name != "Postgres Container" {
		t.Errorf("Name: got %q; the backup wait only retries the Postgres Container check", r.Name)
	}
	if !strings.Contains(r.Message, "not accepting connections") || !strings.Contains(r.Message, "rejecting connections") {
		t.Errorf("Message should say Postgres is not ready and why, got %q", r.Message)
	}
	if r.Remedy == "" {
		t.Error("expected a remedy")
	}
}

func TestCheckPostgresContainer_NotRunningSkipsProbe(t *testing.T) {
	ex := &pgExecutor{running: false}
	r := checkPostgresContainer(ex, "immich_postgres", "postgres")
	if r.OK || !strings.Contains(r.Message, "is not running") {
		t.Errorf("expected a not-running failure, got %+v", r)
	}
	if ex.execCmd != nil {
		t.Errorf("pg_isready must not run in a stopped container, ran %v", ex.execCmd)
	}
}
