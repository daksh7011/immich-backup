// internal/tui/logs_model.go
package tui

import tea "charm.land/bubbletea/v2"

type LogsModel struct{ content string }

func NewLogsModel(content string) LogsModel { return LogsModel{content: content} }

func (m LogsModel) Init() tea.Cmd { return nil }

func (m LogsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "q", "esc", "ctrl+c", "enter":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m LogsModel) View() tea.View {
	out := renderHeader("  Logs  ")
	out += m.content
	out += renderHints([]Hint{{"q / esc / enter", "quit"}})
	return tea.NewView(out)
}
