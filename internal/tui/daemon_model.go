// internal/tui/daemon_model.go
package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

type DaemonModel struct {
	message string
	err     error
}

func NewDaemonModel(message string, err error) DaemonModel {
	return DaemonModel{message: message, err: err}
}

func (m DaemonModel) Init() tea.Cmd                           { return nil }
func (m DaemonModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) { return m, tea.Quit }
func (m DaemonModel) View() string {
	if m.err != nil {
		return errStyle.Render(fmt.Sprintf("Error: %v\n", m.err))
	}
	return okStyle.Render(m.message + "\n")
}
