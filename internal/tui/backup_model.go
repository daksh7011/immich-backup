// internal/tui/backup_model.go
package tui

import (
	"context"
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
	cancel  context.CancelFunc
	done    bool
	lastErr error

	// named phase steps; only shown when the relevant phase is not being skipped
	dbDumpStep     step
	dbUploadStep   step
	mediaScanStep  step
	mediaCheckStep step
	mediaSyncStep  step

	hasDBSteps    bool
	hasMediaSteps bool

	// progress bars
	dbProgress    progress.Model
	mediaProgress progress.Model

	// live progress data
	dbUploadProg *backup.DBUploadProgressMsg
	mediaProg    *backup.MediaProgressMsg
	rcloneErrors []string

	spinner spinner.Model
}

// NewBackupModel creates a BackupModel that reads from ch.
// cancel is called when the user aborts (Ctrl+C).
// skipDB / skipMedia control which step groups are shown.
func NewBackupModel(ch <-chan any, cancel context.CancelFunc, skipDB, skipMedia bool) BackupModel {
	newBar := func() progress.Model {
		p := progress.New(
			progress.WithColors(colorMauve),
			progress.WithoutPercentage(),
		)
		p.SetWidth(48)
		return p
	}

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(colorMauve)

	m := BackupModel{
		ch:            ch,
		cancel:        cancel,
		dbProgress:    newBar(),
		mediaProgress: newBar(),
		spinner:       s,
		hasDBSteps:    !skipDB,
		hasMediaSteps: !skipMedia,
	}

	if !skipDB {
		m.dbDumpStep = step{label: "Dumping database", state: stepPending}
		m.dbUploadStep = step{label: "Uploading database dump", state: stepPending}
	}
	if !skipMedia {
		m.mediaScanStep = step{label: "Scanning library", state: stepPending}
		m.mediaCheckStep = step{label: "Checking for changes", state: stepPending}
		m.mediaSyncStep = step{label: "Syncing media", state: stepPending}
	}

	return m
}

// Err returns the last fatal error received from the backup runner.
func (m BackupModel) Err() error { return m.lastErr }

func (m BackupModel) Init() tea.Cmd {
	return tea.Batch(WaitForChan(m.ch), m.spinner.Tick)
}

func (m BackupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {

	case backup.PhaseMsg:
		switch v.Phase {
		case backup.PhaseDBDump:
			m.dbDumpStep.state = stepRunning
		case backup.PhaseDBUpload:
			m.dbDumpStep.state = stepDone
			m.dbUploadStep.state = stepRunning
		case backup.PhaseMediaScan:
			if m.hasDBSteps {
				m.dbUploadStep.state = stepDone
			}
			m.mediaScanStep.state = stepRunning
		}
		return m, WaitForChan(m.ch)

	case backup.ScanMsg:
		m.mediaScanStep.state = stepDone
		m.mediaScanStep.detail = fmt.Sprintf("%s files, %s",
			formatCount(v.TotalFiles), formatBytes(v.TotalBytes))
		m.mediaCheckStep.state = stepRunning
		return m, WaitForChan(m.ch)

	case backup.DBUploadProgressMsg:
		m.dbUploadProg = &v
		return m, WaitForChan(m.ch)

	case backup.MediaProgressMsg:
		if v.FilesTotal > 0 && m.mediaSyncStep.state == stepPending {
			m.mediaCheckStep.state = stepDone
			m.mediaSyncStep.state = stepRunning
		}
		m.mediaProg = &v
		return m, WaitForChan(m.ch)

	case backup.RcloneErrorMsg:
		m.rcloneErrors = append(m.rcloneErrors, v.Text)
		return m, WaitForChan(m.ch)

	case backup.DoneMsg:
		m.markRunningDone()
		m.done = true
		return m, nil

	case backup.ErrorMsg:
		m.markRunningError(v.Err.Error())
		m.lastErr = v.Err
		m.done = true
		return m, nil

	case chanClosedMsg:
		// Channel closed without DoneMsg — backup was cancelled (ctx cancel).
		// Mark whatever was running as aborted and exit.
		m.markRunningError("aborted")
		m.done = true
		return m, tea.Quit

	case spinner.TickMsg:
		if !m.done {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case tea.KeyMsg:
		if m.done {
			switch v.String() {
			case "q", "enter", "esc", "ctrl+c":
				return m, tea.Quit
			}
			return m, nil
		}
		if v.String() == "ctrl+c" {
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}
	}
	return m, nil
}

// markRunningDone marks all currently-running steps as done.
// mediaCheckStep gets "up to date" detail if no media sync ever started.
func (m *BackupModel) markRunningDone() {
	for _, s := range []*step{&m.dbDumpStep, &m.dbUploadStep, &m.mediaScanStep} {
		if s.state == stepRunning {
			s.state = stepDone
		}
	}
	if m.mediaCheckStep.state == stepRunning {
		m.mediaCheckStep.state = stepDone
		if m.mediaSyncStep.state == stepPending {
			m.mediaCheckStep.detail = "up to date"
		}
	}
	if m.mediaSyncStep.state == stepRunning {
		m.mediaSyncStep.state = stepDone
	}
}

// markRunningError marks the first running step as error with the given detail.
func (m *BackupModel) markRunningError(detail string) {
	for _, s := range []*step{
		&m.dbDumpStep, &m.dbUploadStep,
		&m.mediaScanStep, &m.mediaCheckStep, &m.mediaSyncStep,
	} {
		if s.state == stepRunning {
			s.state = stepError
			s.detail = detail
			return
		}
	}
}

func (m BackupModel) View() tea.View {
	out := renderHeader("  Backup Progress  ")

	// DB steps
	if m.hasDBSteps {
		out += renderOneStep(m.dbDumpStep, m.spinner)
		out += renderOneStep(m.dbUploadStep, m.spinner)
		if m.dbUploadProg != nil {
			out += m.renderDBUploadBar()
		}
	}

	// Media steps
	if m.hasMediaSteps {
		out += renderOneStep(m.mediaScanStep, m.spinner)
		out += renderOneStep(m.mediaCheckStep, m.spinner)
		out += renderOneStep(m.mediaSyncStep, m.spinner)
		if m.mediaProg != nil && m.mediaSyncStep.state != stepPending {
			out += m.renderMediaSyncBar()
		}
	}

	// File-level rclone errors
	if len(m.rcloneErrors) > 0 {
		out += "\n"
		for _, e := range m.rcloneErrors {
			out += " " + errStyle.Render("✗") + " " + errStyle.Render(e) + "\n"
		}
	}

	// Fatal error or completion footer
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

func (m BackupModel) renderDBUploadBar() string {
	p := m.dbUploadProg
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
	speed := formatSpeed(p.Speed)
	eta := formatETA(p.ETA)
	sep := sepStyle.Render("  │  ")
	out := "   " + m.dbProgress.ViewAs(pct) + dimStyle.Render(pctLabel) + "\n"
	out += "   " + dimStyle.Render(speed) + sep + dimStyle.Render(eta) + "\n"
	return out
}

func (m BackupModel) renderMediaSyncBar() string {
	p := m.mediaProg
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
	speed := formatSpeed(p.Speed)
	eta := formatETA(p.ETA)
	files := fmt.Sprintf("%s / %s files",
		formatCount(p.FilesDone),
		formatCount(p.FilesTotal),
	)
	sep := sepStyle.Render("  │  ")
	out := "   " + m.mediaProgress.ViewAs(pct) + dimStyle.Render(pctLabel) + "\n"
	out += "   " + dimStyle.Render(speed) + sep + dimStyle.Render(eta) + sep + dimStyle.Render(files) + "\n"
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
	min := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm%02ds remaining", h, min, s)
	}
	if min > 0 {
		return fmt.Sprintf("%dm%02ds remaining", min, s)
	}
	return fmt.Sprintf("%ds remaining", s)
}

func formatCount(n int64) string {
	switch {
	case n < 1_000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		return fmt.Sprintf("%d,%03d", n/1_000, n%1_000)
	case n < 1_000_000_000:
		return fmt.Sprintf("%d,%03d,%03d", n/1_000_000, (n/1_000)%1_000, n%1_000)
	default:
		return fmt.Sprintf("%d,%03d,%03d,%03d",
			n/1_000_000_000, (n/1_000_000)%1_000, (n/1_000)%1_000, n%1_000)
	}
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
