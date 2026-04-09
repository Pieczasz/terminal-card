package tui

import lg "github.com/charmbracelet/lipgloss"

var (
	StyleBox = lg.NewStyle().
			Border(lg.RoundedBorder()).
			BorderForeground(lg.Color("#874BFD")).
			Padding(1, 2).
			Align(lg.Center)

	StyleTitle = lg.NewStyle().
			Bold(true).
			Foreground(lg.Color("#FAFAFA")).
			Background(lg.Color("#874BFD")).
			Padding(0, 1).
			MarginBottom(1)

	StyleHomePageActionsText = lg.NewStyle().
					Foreground(lg.Color("#6C757D")).
					PaddingRight(3)
)
