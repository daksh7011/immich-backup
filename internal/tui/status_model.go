// internal/tui/status_model.go
package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/daksh7011/immich-backup/internal/status"
)

type StatusModel struct {
	run     *status.LastRun
	nextRun string
}

func NewStatusModel(run *status.LastRun, nextRun string) StatusModel {
	return StatusModel{run: run, nextRun: nextRun}
}

func (m StatusModel) Init() tea.Cmd                           { return nil }
func (m StatusModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) { return m, tea.Quit }
func (m StatusModel) View() tea.View {
	if m.run == nil {
		return tea.NewView("No backup has run yet.\n")
	}
	return tea.NewView(fmt.Sprintf("Last run: %s — %s\nNext run: %s\n",
		m.run.Time.Format("2006-01-02 15:04:05"), m.run.Result, m.nextRun))
}
