// internal/tui/daemon_model.go
package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"
)

// DaemonResultMsg is sent by the goroutine running the daemon operation
// when the operation completes (successfully or with an error).
type DaemonResultMsg struct {
	Msg string // success message shown as step detail; may be empty
	Err error
}

// DaemonModel is the Bubble Tea model for daemon subcommands.
// It shows a spinner while the operation runs, then ✓ or ✗ when done.
type DaemonModel struct {
	ch      <-chan any
	steps   []step // single step for the active operation
	done    bool
	lastErr error
	spinner spinner.Model
}

// NewDaemonModel creates a DaemonModel that reads from ch.
// label is shown while the operation is in progress (e.g. "Installing service…").
func NewDaemonModel(ch <-chan any, label string) DaemonModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(colorMauve)
	return DaemonModel{
		ch:      ch,
		steps:   []step{{label: label, state: stepRunning}},
		spinner: s,
	}
}

// Err returns the error from the daemon operation, if any.
func (m DaemonModel) Err() error { return m.lastErr }

func (m DaemonModel) Init() tea.Cmd {
	return tea.Batch(WaitForChan(m.ch), m.spinner.Tick)
}

func (m DaemonModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {

	case DaemonResultMsg:
		if v.Err != nil {
			m.steps[0].state = stepError
			m.steps[0].detail = v.Err.Error()
			m.lastErr = v.Err
		} else {
			m.steps[0].state = stepDone
			m.steps[0].detail = v.Msg
		}
		m.done = true
		return m, nil

	case chanClosedMsg:
		// Channel closed without DaemonResultMsg — defensive; should not happen
		// in normal operation but prevents a silent hang if it ever does.
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
			case "q", "enter", "esc", "ctrl+c":
				return m, tea.Quit
			}
		}
		if v.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m DaemonModel) View() tea.View {
	out := renderHeader("  Daemon  ")
	out += renderSteps(m.steps, m.spinner)
	if m.done {
		out += renderHints([]Hint{{"q / enter", "quit"}})
	}
	return tea.NewView(out)
}
