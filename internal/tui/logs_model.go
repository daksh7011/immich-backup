// internal/tui/logs_model.go
package tui

import (
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

type LogsModel struct {
	content  string
	viewport viewport.Model
	ready    bool
}

func NewLogsModel(content string) LogsModel {
	return LogsModel{content: content}
}

func (m LogsModel) Init() tea.Cmd { return nil }

func (m LogsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		headerLines := 3
		hintLines := 2
		height := msg.Height - headerLines - hintLines
		if height < 1 {
			height = 1
		}
		if !m.ready {
			m.viewport = viewport.New(viewport.WithWidth(msg.Width), viewport.WithHeight(height))
			m.viewport.SetContent(m.content)
			// start at the bottom so the latest log lines are visible
			m.viewport.GotoBottom()
			m.ready = true
		} else {
			m.viewport.SetWidth(msg.Width)
			m.viewport.SetHeight(height)
		}
		return m, nil
	}

	if m.ready {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m LogsModel) View() tea.View {
	header := renderHeader("  Logs  ")
	hints := renderHints([]Hint{{"↑/↓ / pgup/pgdn", "scroll"}, {"q / esc", "quit"}})
	if !m.ready {
		return tea.NewView(header + hints)
	}
	return tea.NewView(header + m.viewport.View() + "\n" + hints)
}
