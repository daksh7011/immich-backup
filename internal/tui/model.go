// internal/tui/model.go
package tui

import tea "charm.land/bubbletea/v2"

// chanClosedMsg is dispatched when the backing channel is closed with no more values.
// Models that use WaitForChan must handle this to detect completion.
type chanClosedMsg struct{}

// WaitForChan returns a tea.Cmd that reads one message from ch and dispatches it.
// When ch is closed it dispatches chanClosedMsg{}.
func WaitForChan(ch <-chan any) tea.Cmd {
	return func() tea.Msg {
		v, ok := <-ch
		if !ok {
			return chanClosedMsg{}
		}
		return v
	}
}
