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

type overlayMode int

const (
	overlayNone        overlayMode = iota
	overlayErrors                  // ctrl+o — live-updating full error list
	overlayTransferring            // ctrl+t — live-updating full transfer list
)

const overlayVisibleLines = 20

// BackupModel is the Bubble Tea model for live backup progress display.
type BackupModel struct {
	ch      <-chan any
	cancel  context.CancelFunc
	done    bool
	lastErr error

	// named phase steps; only shown when the relevant phase is not being skipped
	dbDumpStep    step
	dbUploadStep  step
	mediaSyncStep step

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

	overlay         overlayMode
	errScrollOffset int
	xfrScrollOffset int
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
		m.mediaSyncStep = step{label: "Syncing media", state: stepPending}
	}

	return m
}

// Err returns the last fatal error received from the backup runner.
func (m BackupModel) Err() error { return m.lastErr }

func (m BackupModel) Init() tea.Cmd {
	cmds := []tea.Cmd{WaitForChan(m.ch)}
	if m.hasDBSteps || m.hasMediaSteps {
		cmds = append(cmds, m.spinner.Tick)
	}
	return tea.Batch(cmds...)
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
		case backup.PhaseMedia:
			if m.hasDBSteps {
				m.dbUploadStep.state = stepDone
			}
			m.mediaSyncStep.state = stepRunning
		}
		return m, WaitForChan(m.ch)

	case backup.DBUploadProgressMsg:
		m.dbUploadProg = &v
		return m, WaitForChan(m.ch)

	case backup.MediaProgressMsg:
		m.mediaProg = &v
		if m.overlay == overlayTransferring {
			maxOff := max(0, len(v.Transferring)-overlayVisibleLines)
			if m.xfrScrollOffset > maxOff {
				m.xfrScrollOffset = maxOff
			}
		}
		return m, WaitForChan(m.ch)

	case backup.RcloneErrorMsg:
		m.rcloneErrors = append(m.rcloneErrors, v.Text)
		if m.overlay == overlayErrors {
			m.errScrollOffset = max(0, len(m.rcloneErrors)-overlayVisibleLines)
		}
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
		// When an overlay is active: only esc and scroll keys are handled.
		// ctrl+o and ctrl+t are explicitly ignored to prevent cross-overlay switching.
		if m.overlay != overlayNone {
			switch v.String() {
			case "esc":
				m.overlay = overlayNone
			case "up", "k":
				switch m.overlay {
				case overlayErrors:
					if m.errScrollOffset > 0 {
						m.errScrollOffset--
					}
				case overlayTransferring:
					if m.xfrScrollOffset > 0 {
						m.xfrScrollOffset--
					}
				}
			case "down", "j":
				switch m.overlay {
				case overlayErrors:
					maxOff := max(0, len(m.rcloneErrors)-overlayVisibleLines)
					if m.errScrollOffset < maxOff {
						m.errScrollOffset++
					}
				case overlayTransferring:
					if m.mediaProg != nil {
						maxOff := max(0, len(m.mediaProg.Transferring)-overlayVisibleLines)
						if m.xfrScrollOffset < maxOff {
							m.xfrScrollOffset++
						}
					}
				}
			}
			return m, nil
		}

		// Main view key handling.
		if m.done {
			switch v.String() {
			case "q", "enter", "esc", "ctrl+c":
				return m, tea.Quit
			}
			return m, nil
		}
		switch v.String() {
		case "ctrl+c":
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "ctrl+o":
			if len(m.rcloneErrors) > 0 {
				m.overlay = overlayErrors
				m.errScrollOffset = max(0, len(m.rcloneErrors)-overlayVisibleLines)
			}
		case "ctrl+t":
			if m.mediaProg != nil && len(m.mediaProg.Transferring) > 0 {
				m.overlay = overlayTransferring
				m.xfrScrollOffset = max(0, len(m.mediaProg.Transferring)-overlayVisibleLines)
			}
		}
	}
	return m, nil
}

// markRunningDone marks all currently-running steps as done.
func (m *BackupModel) markRunningDone() {
	for _, s := range []*step{&m.dbDumpStep, &m.dbUploadStep, &m.mediaSyncStep} {
		if s.state == stepRunning {
			s.state = stepDone
		}
	}
}

// markRunningError marks the first running step as error with the given detail.
func (m *BackupModel) markRunningError(detail string) {
	for _, s := range []*step{&m.dbDumpStep, &m.dbUploadStep, &m.mediaSyncStep} {
		if s.state == stepRunning {
			s.state = stepError
			s.detail = detail
			return
		}
	}
}

func (m BackupModel) View() tea.View {
	switch m.overlay {
	case overlayErrors:
		return tea.NewView(m.renderErrorOverlay())
	case overlayTransferring:
		return tea.NewView(m.renderTransferringOverlay())
	}

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
		out += renderOneStep(m.mediaSyncStep, m.spinner)
		if m.mediaProg != nil {
			out += m.renderMediaProgress()
		}
	}

	// File-level rclone errors — show first 5 inline, hint at the rest.
	if len(m.rcloneErrors) > 0 {
		out += "\n"
		shown := m.rcloneErrors
		remainder := 0
		if len(shown) > 5 {
			shown = shown[:5]
			remainder = len(m.rcloneErrors) - 5
		}
		for _, e := range shown {
			out += " " + errStyle.Render("✗") + " " + errStyle.Render(e) + "\n"
		}
		if remainder > 0 {
			hintSuffix := ""
			if !m.done {
				hintSuffix = "  (ctrl+o to view all)"
			}
			out += " " + warnStyle.Render(fmt.Sprintf("+ %d more errors%s", remainder, hintSuffix)) + "\n"
		}
	}

	// Fatal error or completion footer
	if m.lastErr != nil {
		out += "\n"
		if len(m.rcloneErrors) > 0 {
			// Specific rclone messages are already shown above; avoid repeating
			// the generic "exit status 1" wrapper.
			out += " " + errStyle.Render("✗") + " " + errStyle.Render("Backup failed — see errors above.") + "\n"
		} else {
			out += " " + errStyle.Render("✗") + " " + errStyle.Render(fmt.Sprintf("Error: %v", m.lastErr)) + "\n"
		}
	} else if m.done {
		out += "\n"
		if len(m.rcloneErrors) > 0 {
			out += " " + warnStyle.Render(fmt.Sprintf("✓ Backup complete with %d file error(s).", len(m.rcloneErrors))) + "\n"
		} else {
			out += " " + okStyle.Render("✓ Backup complete!") + "\n"
		}
	}

	if !m.done {
		hints := []Hint{{"ctrl+c", "abort"}}
		if len(m.rcloneErrors) > 0 {
			hints = append(hints, Hint{"ctrl+o", "view errors"})
		}
		if m.mediaProg != nil && len(m.mediaProg.Transferring) > 0 {
			hints = append(hints, Hint{"ctrl+t", "view transfers"})
		}
		out += renderHints(hints)
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

// renderMediaProgress renders the full rclone-style progress block for media sync.
func (m BackupModel) renderMediaProgress() string {
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
	out := "   " + m.mediaProgress.ViewAs(pct) + dimStyle.Render(pctLabel) + "\n\n"

	// Transferred line
	out += "   " + dimStyle.Render(fmt.Sprintf("Transferred:    %s / %s,  %.0f%%,  %s,  ETA %s",
		formatBytes(p.TransferredBytes),
		formatBytes(p.TotalBytes),
		pct*100,
		formatSpeed(p.Speed),
		formatETA(p.ETA),
	)) + "\n"

	// Checks line
	checkPct := ""
	if p.TotalChecks > 0 {
		checkPct = fmt.Sprintf(",  %.0f%%", float64(p.Checks)/float64(p.TotalChecks)*100)
	}
	out += "   " + dimStyle.Render(fmt.Sprintf("Checks:         %s / %s%s",
		formatCount(p.Checks), formatCount(p.TotalChecks), checkPct)) + "\n"

	// Files line
	filePct := ""
	if p.FilesTotal > 0 {
		filePct = fmt.Sprintf(",  %.0f%%", float64(p.FilesDone)/float64(p.FilesTotal)*100)
	}
	out += "   " + dimStyle.Render(fmt.Sprintf("Files:          %s / %s%s",
		formatCount(p.FilesDone), formatCount(p.FilesTotal), filePct)) + "\n"

	// Elapsed time
	out += "   " + dimStyle.Render(fmt.Sprintf("Elapsed time:   %s", formatElapsed(p.ElapsedTime))) + "\n"

	// Active transfers (truncated names)
	if len(p.Transferring) > 0 {
		out += "\n"
		out += "   " + dimStyle.Render("Transferring:") + "\n"
		for _, tf := range p.Transferring {
			name := truncateMid(tf.Name, 40)
			etaStr := "?"
			if tf.ETA != nil {
				etaStr = fmt.Sprintf("%ds", *tf.ETA)
			}
			out += "   " + dimStyle.Render(fmt.Sprintf("  · %-41s %3d%%  /%s  %s  %s",
				name,
				tf.Percentage,
				formatBytes(tf.Size),
				formatSpeed(tf.Speed),
				etaStr,
			)) + "\n"
		}
	}

	return out
}

func (m BackupModel) renderErrorOverlay() string {
	out := renderHeader("  Errors  ")
	total := len(m.rcloneErrors)
	if total == 0 {
		out += dimStyle.Render("  No errors recorded.") + "\n"
	} else {
		end := min(m.errScrollOffset+overlayVisibleLines, total)
		for _, e := range m.rcloneErrors[m.errScrollOffset:end] {
			out += " " + errStyle.Render("✗") + " " + errStyle.Render(e) + "\n"
		}
		if total > overlayVisibleLines {
			out += "\n" + dimStyle.Render(fmt.Sprintf("  %d – %d of %d errors", m.errScrollOffset+1, end, total)) + "\n"
		}
	}
	out += renderHints([]Hint{{"↑/↓  j/k", "scroll"}, {"esc", "back"}})
	return out
}

func (m BackupModel) renderTransferringOverlay() string {
	out := renderHeader("  Transferring  ")
	if m.mediaProg == nil || len(m.mediaProg.Transferring) == 0 {
		out += dimStyle.Render("  No active transfers.") + "\n"
	} else {
		transfers := m.mediaProg.Transferring
		total := len(transfers)
		end := min(m.xfrScrollOffset+overlayVisibleLines, total)
		for _, tf := range transfers[m.xfrScrollOffset:end] {
			etaStr := "?"
			if tf.ETA != nil {
				etaStr = fmt.Sprintf("%ds", *tf.ETA)
			}
			out += " " + dimStyle.Render(fmt.Sprintf("· %s  %3d%%  /%s  %s  %s",
				tf.Name,
				tf.Percentage,
				formatBytes(tf.Size),
				formatSpeed(tf.Speed),
				etaStr,
			)) + "\n"
		}
		if total > overlayVisibleLines {
			out += "\n" + dimStyle.Render(fmt.Sprintf("  %d – %d of %d transfers", m.xfrScrollOffset+1, end, total)) + "\n"
		}
	}
	out += renderHints([]Hint{{"↑/↓  j/k", "scroll"}, {"esc", "back"}})
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

// formatElapsed formats a duration in seconds as a human-readable elapsed string.
func formatElapsed(secs float64) string {
	if secs <= 0 {
		return "0s"
	}
	d := time.Duration(secs * float64(time.Second))
	h := int(d.Hours())
	min := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm%02ds", h, min, s)
	}
	if min > 0 {
		return fmt.Sprintf("%dm%02ds", min, s)
	}
	return fmt.Sprintf("%ds", s)
}

// truncateMid shortens s to at most maxRunes by removing the middle and inserting "…".
// If s is already within the limit it is returned unchanged.
func truncateMid(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	half := (maxRunes - 1) / 2
	tail := maxRunes - half - 1 // accounts for the "…" rune
	return string(runes[:half]) + "…" + string(runes[len(runes)-tail:])
}
