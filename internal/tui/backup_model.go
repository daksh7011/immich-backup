// internal/tui/backup_model.go
package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/daksh7011/immich-backup/internal/backup"
)

// BackupModel is the Bubble Tea model for live backup progress display.
type BackupModel struct {
	ch      <-chan any
	lines   []string
	done    bool
	lastErr error
}

// NewBackupModel creates a BackupModel that reads from ch.
func NewBackupModel(ch <-chan any) BackupModel {
	return BackupModel{ch: ch}
}

// Err returns the last error received from the backup runner.
func (m BackupModel) Err() error { return m.lastErr }

func (m BackupModel) Init() tea.Cmd {
	return WaitForChan(m.ch)
}

func (m BackupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case backup.ProgressMsg:
		m.lines = append(m.lines, v.Text)
		return m, WaitForChan(m.ch)
	case backup.ErrorMsg:
		m.lastErr = v.Err
		m.done = true
		return m, tea.Quit
	case backup.DoneMsg:
		m.done = true
		return m, tea.Quit
	case tea.KeyMsg:
		if v.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m BackupModel) View() tea.View {
	out := renderHeader("  Backup Progress  ")

	arrow := progressStyle.Render("→")
	for _, l := range m.lines {
		out += " " + arrow + " " + dimStyle.Render(l) + "\n"
	}

	if m.lastErr != nil {
		out += " " + errStyle.Render("✗") + " " + errStyle.Render(fmt.Sprintf("Error: %v", m.lastErr)) + "\n"
	} else if m.done {
		out += " " + okStyle.Render("✓") + " " + okStyle.Render("Backup complete!") + "\n"
	}

	if !m.done {
		out += renderHints([]Hint{{"ctrl+c", "abort"}})
	} else {
		out += renderHints([]Hint{{"q / enter", "quit"}})
	}

	return tea.NewView(out)
}
