// internal/tui/doctor_model.go
package tui

import (
	"fmt"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/daksh7011/immich-backup/internal/doctor"
)

// DoctorModel is the Bubble Tea model for the doctor command.
// It receives doctor.CheckStartMsg and doctor.CheckResult values from a channel,
// showing a spinner while each check runs and ✓/✗ when it completes.
type DoctorModel struct {
	ch        <-chan any
	steps     []step
	current   int // index of the currently-running check
	done      bool
	anyFailed bool
	spinner   spinner.Model
}

// AnyFailed returns true if at least one check did not pass.
func (m DoctorModel) AnyFailed() bool { return m.anyFailed }

// NewDoctorModel creates a DoctorModel that reads from ch.
func NewDoctorModel(ch <-chan any) DoctorModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(colorMauve)
	return DoctorModel{ch: ch, spinner: s}
}

func (m DoctorModel) Init() tea.Cmd {
	return WaitForChan(m.ch)
}

func (m DoctorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {

	case doctor.CheckStartMsg:
		m.steps = append(m.steps, step{label: v.Name, state: stepRunning})
		m.current = len(m.steps) - 1
		return m, tea.Batch(WaitForChan(m.ch), m.spinner.Tick)

	case doctor.CheckResult:
		if m.current < len(m.steps) {
			if v.OK {
				m.steps[m.current].state = stepDone
				m.steps[m.current].detail = v.Message
			} else {
				detail := v.Message
				if v.Remedy != "" {
					detail += " → " + v.Remedy
				}
				m.steps[m.current].detail = detail
				if v.Warn {
					// A warning is shown but does not fail doctor.
					m.steps[m.current].state = stepWarn
				} else {
					m.steps[m.current].state = stepError
					m.anyFailed = true
				}
			}
			m.current++ // advance past completed step; guards against duplicate results
		}
		return m, WaitForChan(m.ch)

	case chanClosedMsg:
		m.done = true
		return m, nil

	case spinner.TickMsg:
		if !m.done {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case tea.KeyMsg:
		if m.done {
			switch v.String() {
			case "q", "esc", "ctrl+c", "enter":
				return m, tea.Quit
			}
		}
		if v.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m DoctorModel) View() tea.View {
	out := renderHeader("  Doctor  ")
	out += renderSteps(m.steps, m.spinner)

	if m.done && len(m.steps) > 0 {
		passed, warned := 0, 0
		for _, s := range m.steps {
			switch s.state {
			case stepDone:
				passed++
			case stepWarn:
				warned++
			}
		}
		total := len(m.steps)
		summary := fmt.Sprintf("%d/%d checks passed", passed, total)
		if warned > 0 {
			summary += fmt.Sprintf(", %d warning(s)", warned)
		}
		out += "\n"
		switch {
		case m.anyFailed:
			out += "  " + errStyle.Render(summary) + "\n"
		case warned > 0:
			out += "  " + warnStyle.Render(summary) + "\n"
		default:
			out += "  " + okStyle.Render(summary) + "\n"
		}
		out += renderHints([]Hint{{"q / esc / enter", "quit"}})
	}

	return tea.NewView(out)
}
