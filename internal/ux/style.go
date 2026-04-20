// Package ux owns the CLI's rich-output primitives: palette, spinner,
// progress driver for long-running polls. All exported entry points respect
// the IOStreams ColorEnabled / SpinnersEnabled flags, falling back to plain
// stderr output when the environment (CI, NO_COLOR, pipe) doesn't support
// ANSI.
package ux

import "github.com/charmbracelet/lipgloss"

// Palette defines the CLI-wide colour scheme. Keep it small — three semantic
// colours plus dim — so every phase/status style maps onto the same base.
type Palette struct {
	Success lipgloss.Style
	Warn    lipgloss.Style
	Error   lipgloss.Style
	Dim     lipgloss.Style
}

// DefaultPalette: ANSI bright green / yellow / red + ANSI faint for dim.
// lipgloss honours NO_COLOR and non-TTY streams automatically via its
// detected-profile heuristic, but we also gate every caller by
// IOStreams.ColorEnabled for belt-and-braces consistency.
var DefaultPalette = Palette{
	Success: lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(10)),
	Warn:    lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(11)),
	Error:   lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(9)),
	Dim:     lipgloss.NewStyle().Faint(true),
}

// PhaseStyle returns the palette entry that maps to a ManagedCluster phase.
// Unknown phases fall through to Dim so future server-side phases don't
// break rendering.
func PhaseStyle(phase string) lipgloss.Style {
	switch phase {
	case "Running":
		return DefaultPalette.Success
	case "Provisioning", "Pending", "Upgrading":
		return DefaultPalette.Warn
	case "Degraded", "Terminating":
		return DefaultPalette.Error
	}
	return DefaultPalette.Dim
}

// ColorPhase returns phase either styled or plain, chosen by the caller's
// ColorEnabled flag. Exported so the cluster table printer stays cheap and
// doesn't need to know about lipgloss directly.
func ColorPhase(phase string, colorEnabled bool) string {
	if !colorEnabled {
		return phase
	}
	return PhaseStyle(phase).Render(phase)
}
