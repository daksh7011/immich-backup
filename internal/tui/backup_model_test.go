package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/daksh7011/immich-backup/internal/backup"
)

func TestFormatElapsed(t *testing.T) {
	tests := []struct {
		secs float64
		want string
	}{
		{0, "0s"},
		{-1, "0s"},
		{30, "30s"},
		{60, "1m00s"},
		{90, "1m30s"},
		{3600, "1h00m00s"},
		{3661, "1h01m01s"},
	}
	for _, tt := range tests {
		got := formatElapsed(tt.secs)
		if got != tt.want {
			t.Errorf("formatElapsed(%v) = %q, want %q", tt.secs, got, tt.want)
		}
	}
}

func TestTruncateMid(t *testing.T) {
	tests := []struct {
		s        string
		maxRunes int
		want     string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 7, "hel…rld"},
		{"abcdefghij", 5, "ab…ij"},
		{"abc", 0, ""},
		{"résumé", 4, "r…mé"},
	}
	for _, tt := range tests {
		got := truncateMid(tt.s, tt.maxRunes)
		if got != tt.want {
			t.Errorf("truncateMid(%q, %d) = %q, want %q", tt.s, tt.maxRunes, got, tt.want)
		}
	}
}

func TestBackupModel_PartialMediaSync_ShowsIncomplete(t *testing.T) {
	m := NewBackupModel(nil, nil, true, false)
	var model tea.Model = m
	model, _ = model.Update(backup.PhaseMsg{Phase: backup.PhaseMedia})
	model, _ = model.Update(backup.RcloneErrorMsg{Text: "photo.jpg: read failed"})
	pe := &backup.PartialError{FileErrors: 1, Err: errors.New("exit status 1")}
	model, _ = model.Update(backup.ErrorMsg{Err: fmt.Errorf("media sync: %w", pe)})

	final := model.(BackupModel)
	if final.Err() == nil {
		t.Fatal("partial sync must surface as an error")
	}
	view := final.View().Content
	if !strings.Contains(view, "Backup incomplete") {
		t.Errorf("view should report an incomplete backup, got:\n%s", view)
	}
	if strings.Contains(view, "Backup failed") || strings.Contains(view, "Backup complete") {
		t.Errorf("view must not claim the backup completed, got:\n%s", view)
	}
}

func TestBackupModel_FatalError_ShowsFailed(t *testing.T) {
	m := NewBackupModel(nil, nil, true, false)
	var model tea.Model = m
	model, _ = model.Update(backup.PhaseMsg{Phase: backup.PhaseMedia})
	model, _ = model.Update(backup.ErrorMsg{Err: errors.New("media sync: upload_location /x is empty")})

	view := model.(BackupModel).View().Content
	if !strings.Contains(view, "upload_location /x is empty") {
		t.Errorf("view should show the fatal error, got:\n%s", view)
	}
}
