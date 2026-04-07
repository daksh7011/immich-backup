// internal/tui/model.go
package tui

import tea "github.com/charmbracelet/bubbletea"

// WaitForChan returns a tea.Cmd that reads one message from ch and dispatches it.
func WaitForChan(ch <-chan any) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}
