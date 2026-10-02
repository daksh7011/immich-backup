// internal/tui/status_model.go
package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/daksh7011/immich-backup/internal/status"
)

// ServiceInfo is the background service as the status screen shows it.
type ServiceInfo struct {
	State    string   // e.g. "active (waiting), enabled", "not installed"
	NextRun  string   // next scheduled run, or why there is none
	Problems []string // problems that stop scheduled backups, each with a remedy
}

type StatusModel struct {
	run     *status.LastRun
	service ServiceInfo
}

func NewStatusModel(run *status.LastRun, service ServiceInfo) StatusModel {
	return StatusModel{run: run, service: service}
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
			dimStyle.Render(formatRunTime(m.run.Time))+" "+resultStyle.Render("["+m.run.Result+"]"))
		if m.run.Result != status.ResultSuccess {
			// The last attempt failed: say when data was last backed up in full.
			lastOK := "never recorded"
			if !m.run.LastSuccess.IsZero() {
				lastOK = formatRunTime(m.run.LastSuccess)
			}
			out += fmt.Sprintf("  %s  %s\n",
				dimStyle.Render("Last ok: "),
				dimStyle.Render(lastOK))
		}
		if m.run.Error != "" {
			out += fmt.Sprintf("  %s  %s\n",
				dimStyle.Render("Error:   "),
				errStyle.Render(m.run.Error))
		}
	}

	// The service and next run are shown even before the first backup, since
	// "nothing has run yet" is exactly when the schedule needs checking.
	if m.service.State != "" {
		out += fmt.Sprintf("  %s  %s\n",
			dimStyle.Render("Service: "),
			dimStyle.Render(m.service.State))
	}
	if m.service.NextRun != "" {
		out += fmt.Sprintf("  %s  %s\n",
			dimStyle.Render("Next run:"),
			dimStyle.Render(m.service.NextRun))
	}
	for _, p := range m.service.Problems {
		out += fmt.Sprintf("  %s  %s\n",
			warnStyle.Render("Problem: "),
			warnStyle.Render(p))
	}

	out += renderHints([]Hint{{"q / esc / enter", "quit"}})
	return tea.NewView(out)
}

// formatRunTime shows a recorded run time (stored in UTC) in local time with
// its zone, matching how the next scheduled run is shown.
func formatRunTime(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04:05 MST")
}
