// internal/tui/styles.go
package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Catppuccin Mocha palette
var (
	colorMauve    = lipgloss.Color("#CBA6F7")
	colorGreen    = lipgloss.Color("#A6E3A1")
	colorRed      = lipgloss.Color("#F38BA8")
	colorYellow   = lipgloss.Color("#F9E2AF")
	colorSubtext  = lipgloss.Color("#A6ADC8")
	colorOverlay  = lipgloss.Color("#6C7086")
	colorBase     = lipgloss.Color("#1E1E2E")
	colorSurface1 = lipgloss.Color("#45475A")
)

// Semantic styles shared across models.
var (
	okStyle   = lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
	errStyle  = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	warnStyle = lipgloss.NewStyle().Foreground(colorYellow)

	headerStyle = lipgloss.NewStyle().
			Background(colorMauve).
			Foreground(colorBase).
			Bold(true).
			Padding(0, 2)

	keyStyle  = lipgloss.NewStyle().Foreground(colorMauve).Bold(true)
	descStyle = lipgloss.NewStyle().Foreground(colorOverlay)
	dimStyle  = lipgloss.NewStyle().Foreground(colorSubtext)

	progressStyle = lipgloss.NewStyle().Foreground(colorMauve)
	sepStyle      = lipgloss.NewStyle().Foreground(colorSurface1)
)

// Hint is a single key binding shown in the hint bar.
type Hint struct {
	Key  string
	Desc string
}

// renderHeader renders a styled title bar.
func renderHeader(title string) string {
	return headerStyle.Render(title) + "\n\n"
}

// renderHints renders a bottom key-hint bar.
func renderHints(hints []Hint) string {
	parts := make([]string, len(hints))
	for i, h := range hints {
		parts[i] = keyStyle.Render(h.Key) + " " + descStyle.Render(h.Desc)
	}
	sep := sepStyle.Render("  │  ")
	return "\n" + dimStyle.Render("  ") + strings.Join(parts, sep)
}
