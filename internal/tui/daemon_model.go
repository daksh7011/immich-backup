// internal/tui/daemon_model.go
package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

type DaemonModel struct {
	message string
	err     error
}

func NewDaemonModel(message string, err error) DaemonModel {
	return DaemonModel{message: message, err: err}
}

func (m DaemonModel) Init() tea.Cmd { return nil }

func (m DaemonModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "q", "esc", "ctrl+c", "enter":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m DaemonModel) View() tea.View {
	out := renderHeader("  Daemon  ")
	if m.err != nil {
		out += "  " + errStyle.Render(fmt.Sprintf("✗  %v", m.err)) + "\n"
	} else {
		out += "  " + okStyle.Render("✓  "+m.message) + "\n"
	}
	out += renderHints([]Hint{{"q / esc / enter", "quit"}})
	return tea.NewView(out)
}
