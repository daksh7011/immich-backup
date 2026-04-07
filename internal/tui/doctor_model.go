// internal/tui/doctor_model.go
package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/daksh7011/immich-backup/internal/doctor"
)

type DoctorModel struct{ results []doctor.CheckResult }

func NewDoctorModel(results []doctor.CheckResult) DoctorModel {
	return DoctorModel{results: results}
}

func (m DoctorModel) Init() tea.Cmd { return nil }

func (m DoctorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "q", "esc", "ctrl+c", "enter":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m DoctorModel) View() tea.View {
	out := renderHeader("  Doctor  ")

	passed := 0
	for _, r := range m.results {
		icon := okStyle.Render("✓")
		label := dimStyle.Render(fmt.Sprintf("%-22s", r.Name))
		if r.OK {
			passed++
			out += fmt.Sprintf("  %s  %s %s\n", icon, label, dimStyle.Render(r.Message))
		} else {
			icon = errStyle.Render("✗")
			out += fmt.Sprintf("  %s  %s %s\n", icon, label, errStyle.Render(r.Message))
			if r.Remedy != "" {
				out += fmt.Sprintf("       %s\n", warnStyle.Render("→ "+r.Remedy))
			}
		}
	}

	out += "\n"
	total := len(m.results)
	summary := fmt.Sprintf("%d/%d checks passed", passed, total)
	if passed == total {
		out += "  " + okStyle.Render(summary) + "\n"
	} else {
		out += "  " + errStyle.Render(summary) + "\n"
	}

	out += renderHints([]Hint{{"q / esc / enter", "quit"}})
	return tea.NewView(out)
}
