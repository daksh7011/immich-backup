// internal/tui/steps.go
package tui

import "charm.land/bubbles/v2/spinner"

type stepState int

const (
	stepPending stepState = iota
	stepRunning
	stepDone
	stepError
)

type step struct {
	label  string
	state  stepState
	detail string // optional sub-line: speed, ETA, error text, "up to date", etc.
}

// renderOneStep renders a single step line with the spinner used for running state.
func renderOneStep(s step, sp spinner.Model) string {
	switch s.state {
	case stepRunning:
		return " " + sp.View() + " " + dimStyle.Render(s.label) + "\n"
	case stepDone:
		detail := ""
		if s.detail != "" {
			detail = "  " + dimStyle.Render(s.detail)
		}
		return " " + okStyle.Render("✓") + " " + dimStyle.Render(s.label) + detail + "\n"
	case stepError:
		detail := ""
		if s.detail != "" {
			detail = "  " + errStyle.Render(s.detail)
		}
		return " " + errStyle.Render("✗") + " " + errStyle.Render(s.label) + detail + "\n"
	default: // stepPending
		return " " + sepStyle.Render("·") + " " + dimStyle.Render(s.label) + "\n"
	}
}

// renderSteps renders a list of steps in order.
func renderSteps(steps []step, sp spinner.Model) string {
	out := ""
	for _, s := range steps {
		out += renderOneStep(s, sp)
	}
	return out
}
