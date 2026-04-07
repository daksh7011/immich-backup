// internal/tui/model.go
package tui

import tea "charm.land/bubbletea/v2"

// WaitForChan returns a tea.Cmd that reads one message from ch and dispatches it.
func WaitForChan(ch <-chan any) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}
