// internal/tui/setup_model.go
package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/daksh7011/immich-backup/internal/config"
)

// SetupModel collects initial configuration via a Huh form.
type SetupModel struct {
	form   *huh.Form
	result *config.Config
	done   bool
}

// NewSetupModel creates a SetupModel pre-populated with defaults from cfg.
func NewSetupModel(cfg *config.Config) SetupModel {
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Immich upload location").
				Value(&cfg.Immich.UploadLocation),
			huh.NewInput().
				Title("Postgres container name").
				Value(&cfg.Immich.PostgresContainer),
			huh.NewInput().
				Title("Postgres user").
				Value(&cfg.Immich.PostgresUser),
			huh.NewInput().
				Title("Postgres database").
				Value(&cfg.Immich.PostgresDB),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("rclone remote (e.g. b2-encrypted:immich-backup)").
				Value(&cfg.Backup.RcloneRemote),
			huh.NewInput().
				Title("Backup schedule (cron)").
				Value(&cfg.Backup.Schedule),
			huh.NewInput().
				Title("DB backup frequency (cron)").
				Value(&cfg.Backup.DBBackupFrequency),
		),
	)
	return SetupModel{form: form, result: cfg}
}

func (m SetupModel) Init() tea.Cmd          { return m.form.Init() }
func (m SetupModel) Result() *config.Config { return m.result }
func (m SetupModel) Done() bool             { return m.done }

func (m SetupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	form, cmd := m.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		m.form = f
		switch m.form.State {
		case huh.StateCompleted:
			m.done = true
			return m, tea.Quit
		case huh.StateAborted:
			return m, tea.Quit
		}
	}
	return m, cmd
}

func (m SetupModel) View() tea.View {
	return tea.NewView(renderHeader("  Setup  ") + m.form.View())
}
