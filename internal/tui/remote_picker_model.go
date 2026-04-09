// internal/tui/remote_picker_model.go
package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// RemotePickerModel presents a select + path input so the user can choose
// a one-shot rclone remote destination for a single backup run.
type RemotePickerModel struct {
	form       *huh.Form
	done       bool
	aborted    bool
	remoteName *string
	remotePath *string
}

// NewRemotePickerModel constructs the picker. remotes is the list of bare
// remote names (no trailing colon). defaultRemote is the current configured
// value (e.g. "b2-encrypted:immich-backup") used to pre-fill both fields.
func NewRemotePickerModel(remotes []string, defaultRemote string) RemotePickerModel {
	if len(remotes) == 0 {
		panic("NewRemotePickerModel: remotes must not be empty")
	}
	rn := new(string)
	rp := new(string)
	name, path := splitRemote(defaultRemote)
	if !slices.Contains(remotes, name) {
		name = remotes[0]
	}
	*rn = name
	*rp = path

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select rclone remote").
				Options(huh.NewOptions[string](remotes...)...).
				Value(rn),
			huh.NewInput().
				Title("Path / bucket (e.g. immich-backup)").
				Value(rp),
		),
	)
	return RemotePickerModel{form: form, remoteName: rn, remotePath: rp}
}

// Result returns the combined "name:path" string chosen by the user.
// Must only be called after Done() returns true; always construct via NewRemotePickerModel.
func (m RemotePickerModel) Result() string {
	return *m.remoteName + ":" + *m.remotePath
}

// Done reports whether the user completed the form.
func (m RemotePickerModel) Done() bool { return m.done }

// Aborted reports whether the user cancelled the picker.
func (m RemotePickerModel) Aborted() bool { return m.aborted }

func (m RemotePickerModel) Init() tea.Cmd { return m.form.Init() }

func (m RemotePickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	form, cmd := m.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		m.form = f
		switch m.form.State {
		case huh.StateCompleted:
			m.done = true
			return m, tea.Quit
		case huh.StateAborted:
			m.aborted = true
			return m, tea.Quit
		}
	}
	return m, cmd
}

func (m RemotePickerModel) View() tea.View {
	return tea.NewView(renderHeader("  Select Remote  ") + m.form.View() + renderHints([]Hint{{"enter", "confirm"}, {"ctrl+c", "cancel"}}))
}
