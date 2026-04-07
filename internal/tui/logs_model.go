// internal/tui/logs_model.go
package tui

import tea "charm.land/bubbletea/v2"

type LogsModel struct{ content string }

func NewLogsModel(content string) LogsModel                    { return LogsModel{content: content} }
func (m LogsModel) Init() tea.Cmd                              { return nil }
func (m LogsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd)   { return m, tea.Quit }
func (m LogsModel) View() tea.View                               { return tea.NewView(m.content) }
