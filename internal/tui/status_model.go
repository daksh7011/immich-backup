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

func (m StatusModel) Init() tea.Cmd { return nil }

func (m StatusModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "q", "esc", "ctrl+c", "enter":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m StatusModel) View() tea.View {
	out := renderHeader("  Status  ")

	if m.run == nil {
		out += "  " + dimStyle.Render("No backup has run yet.") + "\n"
	} else {
		resultStyle := errStyle
		switch m.run.Result {
		case status.ResultSuccess:
			resultStyle = okStyle
		case status.ResultPartial:
			resultStyle = warnStyle
		}
		out += fmt.Sprintf("  %s  %s\n",
			dimStyle.Render("Last run:"),
			dimStyle.Render(m.run.Time.Format("2006-01-02 15:04:05"))+" "+resultStyle.Render("["+m.run.Result+"]"))
		if m.run.Result != status.ResultSuccess {
			// The last attempt failed: say when data was last backed up in full.
			lastOK := "never recorded"
			if !m.run.LastSuccess.IsZero() {
				lastOK = m.run.LastSuccess.Format("2006-01-02 15:04:05")
			}
			out += fmt.Sprintf("  %s  %s\n",
				dimStyle.Render("Last ok: "),
				dimStyle.Render(lastOK))
		}
		out += fmt.Sprintf("  %s  %s\n",
			dimStyle.Render("Next run:"),
			dimStyle.Render(m.nextRun))
		if m.run.Error != "" {
			out += fmt.Sprintf("  %s  %s\n",
				dimStyle.Render("Error:   "),
				errStyle.Render(m.run.Error))
		}
	}

	out += renderHints([]Hint{{"q / esc / enter", "quit"}})
	return tea.NewView(out)
}
