// internal/tui/daemon_model.go
package tui

import (
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// DaemonResultMsg is sent by the goroutine running the daemon operation
// when the operation completes (successfully or with an error).
type DaemonResultMsg struct {
	Msg string // result text; a single line is the step detail, several lines a block below it
	Err error  // shown on the step; Msg is still shown, e.g. the status that explains the error
}

// DaemonModel is the Bubble Tea model for daemon subcommands.
// It shows a spinner while the operation runs, then ✓ or ✗ when done.
type DaemonModel struct {
	ch      <-chan any
	steps   []step // single step for the active operation
	done    bool
	lastErr error
	body    string // multi-line result text rendered below the step
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
		// Msg is kept even when Err is set: for `daemon status` it is the
		// state that explains the error.
		text := strings.TrimRight(v.Msg, "\n")
		if strings.Contains(text, "\n") || (v.Err != nil && text != "") {
			m.body = text
		} else {
			m.steps[0].detail = text
		}
		if v.Err != nil {
			m.steps[0].state = stepError
			// Indent continuation lines of a multi-problem error under the step.
			m.steps[0].detail = strings.ReplaceAll(v.Err.Error(), "\n", "\n    ")
			m.lastErr = v.Err
		} else {
			m.steps[0].state = stepDone
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
	if m.body != "" {
		out += "\n"
		for _, line := range strings.Split(m.body, "\n") {
			out += "   " + line + "\n"
		}
	}
	if m.done {
		out += renderHints([]Hint{{"q / enter", "quit"}})
	}
	return tea.NewView(out)
}
