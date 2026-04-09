package tui

import "github.com/charmbracelet/lipgloss"

var (
	StyleBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#874BFD")).
			Padding(1, 2).
			Align(lipgloss.Center)

	StyleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#874BFD")).
			Padding(0, 1).
			MarginBottom(1)
)
