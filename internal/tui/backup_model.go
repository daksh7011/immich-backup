// internal/tui/backup_model.go
package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"
	"github.com/daksh7011/immich-backup/internal/backup"
)

// BackupModel is the Bubble Tea model for live backup progress display.
type BackupModel struct {
	ch      <-chan any
	lines   []string
	done    bool
	lastErr error

	// progress tracking
	progress     progress.Model
	spinner      spinner.Model
	scanning     bool
	totalBytes   int64
	totalFiles   int64
	mediaProg    *backup.MediaProgressMsg
	rcloneErrors []string
}

// NewBackupModel creates a BackupModel that reads from ch.
func NewBackupModel(ch <-chan any) BackupModel {
	p := progress.New(
		progress.WithColors(
			lipgloss.Color("#CBA6F7"), // colorMauve — filled portion
			lipgloss.Color("#CBA6F7"), // single color (no gradient)
		),
		progress.WithoutPercentage(), // we render percentage ourselves in the stats row
	)
	p.SetWidth(48)

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#CBA6F7"))

	return BackupModel{
		ch:       ch,
		progress: p,
		spinner:  s,
	}
}

// Err returns the last fatal error received from the backup runner.
func (m BackupModel) Err() error { return m.lastErr }

func (m BackupModel) Init() tea.Cmd {
	return WaitForChan(m.ch)
}

func (m BackupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {

	case backup.ScanMsg:
		m.scanning = true
		m.totalFiles = v.TotalFiles
		m.totalBytes = v.TotalBytes
		return m, tea.Batch(WaitForChan(m.ch), m.spinner.Tick)

	case backup.MediaProgressMsg:
		m.scanning = false
		m.mediaProg = &v
		return m, WaitForChan(m.ch)

	case backup.RcloneErrorMsg:
		m.rcloneErrors = append(m.rcloneErrors, v.Text)
		return m, WaitForChan(m.ch)

	case backup.ProgressMsg:
		m.lines = append(m.lines, v.Text)
		return m, WaitForChan(m.ch)

	case backup.ErrorMsg:
		m.scanning = false
		m.lastErr = v.Err
		m.done = true
		return m, tea.Quit

	case backup.DoneMsg:
		m.scanning = false
		m.done = true
		return m, tea.Quit

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		if v.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m BackupModel) View() tea.View {
	out := renderHeader("  Backup Progress  ")

	// Text log lines (database steps, etc.)
	arrow := progressStyle.Render("→")
	for _, l := range m.lines {
		out += " " + arrow + " " + dimStyle.Render(l) + "\n"
	}

	// Progress section (only shown once scanning or syncing has started)
	if m.scanning || m.mediaProg != nil || (m.done && m.totalBytes > 0) {
		out += "\n"
		out += m.renderProgressSection()
	}

	// File-level rclone errors
	if len(m.rcloneErrors) > 0 {
		out += "\n"
		for _, e := range m.rcloneErrors {
			out += " " + errStyle.Render("✗") + " " + errStyle.Render(e) + "\n"
		}
	}

	// Fatal error or completion
	if m.lastErr != nil {
		out += "\n " + errStyle.Render("✗") + " " + errStyle.Render(fmt.Sprintf("Error: %v", m.lastErr)) + "\n"
	} else if m.done {
		out += "\n"
		if len(m.rcloneErrors) > 0 {
			out += " " + warnStyle.Render(fmt.Sprintf("✓ Backup complete with %d file error(s).", len(m.rcloneErrors))) + "\n"
		} else {
			out += " " + okStyle.Render("✓ Backup complete!") + "\n"
		}
	}

	if !m.done {
		out += renderHints([]Hint{{"ctrl+c", "abort"}})
	} else {
		out += renderHints([]Hint{{"q / enter", "quit"}})
	}

	return tea.NewView(out)
}

func (m BackupModel) renderProgressSection() string {
	out := ""

	if m.scanning {
		// Indeterminate: spinner + label
		out += " " + m.spinner.View() + " " + dimStyle.Render("Scanning library...") + "\n"
		return out
	}

	if m.mediaProg == nil {
		return out
	}

	p := m.mediaProg

	// Determinate progress bar
	pct := 0.0
	if p.TotalBytes > 0 {
		pct = float64(p.TransferredBytes) / float64(p.TotalBytes)
		if pct > 1.0 {
			pct = 1.0
		}
	}
	if m.done {
		pct = 1.0
	}
	pctLabel := fmt.Sprintf(" %3.0f%%", pct*100)
	out += " " + m.progress.ViewAs(pct) + dimStyle.Render(pctLabel) + "\n"

	// Stats row: speed | ETA | files
	speed := formatSpeed(p.Speed)
	eta := formatETA(p.ETA)
	files := fmt.Sprintf("%s / %s files",
		formatCount(p.FilesDone),
		formatCount(p.FilesTotal),
	)
	sep := sepStyle.Render("  │  ")
	out += " " + dimStyle.Render(speed) + sep + dimStyle.Render(eta) + sep + dimStyle.Render(files) + "\n"

	return out
}

func formatSpeed(bytesPerSec float64) string {
	if bytesPerSec <= 0 {
		return "0 B/s"
	}
	switch {
	case bytesPerSec >= 1<<30:
		return fmt.Sprintf("%.1f GB/s", bytesPerSec/(1<<30))
	case bytesPerSec >= 1<<20:
		return fmt.Sprintf("%.1f MB/s", bytesPerSec/(1<<20))
	case bytesPerSec >= 1<<10:
		return fmt.Sprintf("%.1f KB/s", bytesPerSec/(1<<10))
	default:
		return fmt.Sprintf("%.0f B/s", bytesPerSec)
	}
}

func formatETA(eta *int64) string {
	if eta == nil {
		return "calculating..."
	}
	d := time.Duration(*eta) * time.Second
	if d <= 0 {
		return "done"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm%02ds remaining", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%02ds remaining", m, s)
	}
	return fmt.Sprintf("%ds remaining", s)
}

func formatCount(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1_000_000 {
		return fmt.Sprintf("%d,%03d", n/1000, n%1000)
	}
	return fmt.Sprintf("%d,%03d,%03d", n/1_000_000, (n/1000)%1000, n%1000)
}
