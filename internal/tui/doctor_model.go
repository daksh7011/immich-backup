// internal/tui/doctor_model.go
package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/daksh7011/immich-backup/internal/doctor"
)

type DoctorModel struct{ results []doctor.CheckResult }

func NewDoctorModel(results []doctor.CheckResult) DoctorModel {
	return DoctorModel{results: results}
}

func (m DoctorModel) Init() tea.Cmd                           { return nil }
func (m DoctorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) { return m, tea.Quit }
func (m DoctorModel) View() string {
	out := ""
	for _, r := range m.results {
		icon := okStyle.Render("✓")
		if !r.OK {
			icon = errStyle.Render("✗")
		}
		out += fmt.Sprintf("%s  %-20s %s\n", icon, r.Name, r.Message)
		if !r.OK && r.Remedy != "" {
			out += fmt.Sprintf("   → %s\n", r.Remedy)
		}
	}
	return out
}
