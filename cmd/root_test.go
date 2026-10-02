package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
)

func TestReportError_PrintsPlainError(t *testing.T) {
	var buf bytes.Buffer
	reportError(&buf, errors.New("boom"))
	if buf.String() != "Error: boom\n" {
		t.Errorf("got %q", buf.String())
	}
}

// A TUI that already showed the error must not have it repeated on stderr,
// even when the shown error was wrapped again on the way out.
func TestReportError_SkipsShownError(t *testing.T) {
	var buf bytes.Buffer
	inner := errors.New("the backup timer is inactive")
	err := fmt.Errorf("daemon: %w", shownError{inner})
	reportError(&buf, err)
	if buf.Len() != 0 {
		t.Errorf("shown error printed again: %q", buf.String())
	}
	if !errors.Is(err, inner) {
		t.Error("a wrapped shownError must still match the original error")
	}
}
