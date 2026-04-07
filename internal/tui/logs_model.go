// internal/tui/logs_model.go
package tui

import tea "github.com/charmbracelet/bubbletea"

type LogsModel struct{ content string }

func NewLogsModel(content string) LogsModel                    { return LogsModel{content: content} }
func (m LogsModel) Init() tea.Cmd                              { return nil }
func (m LogsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd)   { return m, tea.Quit }
func (m LogsModel) View() string                               { return m.content }
