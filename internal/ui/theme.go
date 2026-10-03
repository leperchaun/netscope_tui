package ui

import "github.com/charmbracelet/lipgloss"

// ApplyTheme switches to a named palette. "default" keeps the colours; "mono" uses weight only,
// for terminals without colour or for pasting output into plain-text logs.
func ApplyTheme(name string) {
	if name != "mono" {
		return
	}
	plain := lipgloss.NewStyle()
	accentB = lipgloss.NewStyle().Bold(true)
	bold = lipgloss.NewStyle().Bold(true)
	muted = plain
	dim = plain
	border = plain
	okStyle = plain
	warn = lipgloss.NewStyle().Bold(true)
	badStyle = lipgloss.NewStyle().Bold(true).Underline(true)
	rxStyle = plain
	txStyle = plain
}
