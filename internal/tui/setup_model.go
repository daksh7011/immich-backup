// internal/tui/setup_model.go
package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/rcloneconf"
)

// SetupModel collects initial configuration via a Huh form.
type SetupModel struct {
	form         *huh.Form
	result       *config.Config
	done         bool
	useSelect    bool
	remoteName   *string
	remotePath   *string
	transfersStr *string
	checkersStr  *string
}

// NewSetupModel creates a SetupModel pre-populated with defaults from cfg.
// rcloneConfigPath is used to populate the remote name selector; if it cannot
// be read the field falls back to a plain text input.
func NewSetupModel(cfg *config.Config, rcloneConfigPath string) SetupModel {
	remoteName, remotePath := splitRemote(cfg.Backup.RcloneRemote)
	remotes, err := rcloneconf.ListRemotes(rcloneConfigPath)
	useSelect := err == nil && len(remotes) > 0

	m := SetupModel{result: cfg, useSelect: useSelect}

	ts := new(string)
	cs := new(string)
	*ts = strconv.Itoa(cfg.Backup.Transfers)
	*cs = strconv.Itoa(cfg.Backup.Checkers)
	m.transfersStr = ts
	m.checkersStr = cs

	perfGroup := huh.NewGroup(
		huh.NewInput().
			Title("Parallel file transfers").
			Value(ts).
			Validate(func(s string) error {
				v, err := strconv.Atoi(s)
				if err != nil || v <= 0 {
					return fmt.Errorf("must be a positive integer")
				}
				return nil
			}),
		huh.NewInput().
			Title("Parallel checkers").
			Value(cs).
			Validate(func(s string) error {
				v, err := strconv.Atoi(s)
				if err != nil || v <= 0 {
					return fmt.Errorf("must be a positive integer")
				}
				return nil
			}),
		huh.NewInput().
			Title("Buffer size (e.g. 64M)").
			Value(&cfg.Backup.BufferSize).
			Validate(func(s string) error {
				if s == "" {
					return fmt.Errorf("must not be empty")
				}
				return nil
			}),
	)

	var remoteGroup *huh.Group
	if useSelect {
		if !slices.Contains(remotes, remoteName) {
			remoteName = remotes[0]
		}
		rn := new(string)
		rp := new(string)
		*rn = remoteName
		*rp = remotePath
		m.remoteName = rn
		m.remotePath = rp
		remoteGroup = huh.NewGroup(
			huh.NewSelect[string]().
				Title("rclone remote name").
				Options(huh.NewOptions[string](remotes...)...).
				Value(rn),
			huh.NewInput().
				Title("Path / bucket (e.g. immich-backup)").
				Value(rp),
			huh.NewInput().
				Title("Backup schedule (cron)").
				Value(&cfg.Backup.Schedule),
			huh.NewInput().
				Title("DB backup frequency (cron)").
				Value(&cfg.Backup.DBBackupFrequency),
		)
	} else {
		remoteGroup = huh.NewGroup(
			huh.NewInput().
				Title("rclone remote (e.g. b2-encrypted:immich-backup)").
				Value(&cfg.Backup.RcloneRemote),
			huh.NewInput().
				Title("Backup schedule (cron)").
				Value(&cfg.Backup.Schedule),
			huh.NewInput().
				Title("DB backup frequency (cron)").
				Value(&cfg.Backup.DBBackupFrequency),
		)
	}

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
		remoteGroup,
		perfGroup,
	)
	m.form = form
	return m
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
			if m.useSelect {
				m.result.Backup.RcloneRemote = *m.remoteName + ":" + *m.remotePath
			}
			if v, err := strconv.Atoi(*m.transfersStr); err == nil {
				m.result.Backup.Transfers = v
			}
			if v, err := strconv.Atoi(*m.checkersStr); err == nil {
				m.result.Backup.Checkers = v
			}
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

// splitRemote splits a rclone remote string "name:path" into its components.
// For "b2-encrypted:immich-backup" it returns ("b2-encrypted", "immich-backup").
// If there is no colon, the whole string is the name and path is empty.
func splitRemote(remote string) (name, path string) {
	if i := strings.IndexByte(remote, ':'); i >= 0 {
		return remote[:i], remote[i+1:]
	}
	return remote, ""
}
