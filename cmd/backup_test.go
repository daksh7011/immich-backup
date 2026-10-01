// cmd/backup_test.go
package cmd

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/daksh7011/immich-backup/internal/backup"
	"github.com/daksh7011/immich-backup/internal/status"
)

func TestRunOutcome(t *testing.T) {
	partial := fmt.Errorf("media sync: %w",
		&backup.PartialError{FileErrors: 3, Err: errors.New("exit status 1")})

	tests := []struct {
		name       string
		err        error
		wantResult string
	}{
		{"success", nil, status.ResultSuccess},
		{"partial media sync", partial, status.ResultPartial},
		{"fatal error", errors.New("database backup: boom"), status.ResultError},
		{"aborted", context.Canceled, status.ResultError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := &status.LastRun{}
			recordOutcome(run, tt.err)
			if run.Result != tt.wantResult {
				t.Errorf("Result: got %q, want %q", run.Result, tt.wantResult)
			}
			if tt.err == nil && run.Error != "" {
				t.Errorf("Error: got %q, want empty on success", run.Error)
			}
			if tt.err != nil && run.Error != tt.err.Error() {
				t.Errorf("Error: got %q, want %q", run.Error, tt.err.Error())
			}
		})
	}
}

func TestRunBackupHeadless_PartialErrorIsReturned(t *testing.T) {
	ch := make(chan any, 4)
	pe := &backup.PartialError{FileErrors: 1, Err: errors.New("exit status 1")}
	ch <- backup.RcloneErrorMsg{Text: "photo.jpg: read failed"}
	ch <- backup.ErrorMsg{Err: fmt.Errorf("media sync: %w", pe)}
	close(ch)

	err := runBackupHeadless(context.Background(), ch)
	var got *backup.PartialError
	if !errors.As(err, &got) {
		t.Fatalf("expected *PartialError so the exit code is non-zero, got %v", err)
	}
}
