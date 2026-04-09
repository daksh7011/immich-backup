// internal/tui/configure_model.go
package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/daksh7011/immich-backup/internal/config"
)

// ConfigureModel wraps SetupModel for the configure command.
type ConfigureModel struct{ SetupModel }

func NewConfigureModel(cfg *config.Config, rcloneConfigPath string) ConfigureModel {
	return ConfigureModel{SetupModel: NewSetupModel(cfg, rcloneConfigPath)}
}

func (m ConfigureModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	inner, cmd := m.SetupModel.Update(msg)
	m.SetupModel = inner.(SetupModel)
	return m, cmd
}
