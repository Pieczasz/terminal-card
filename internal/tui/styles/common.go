package styles

import lg "github.com/charmbracelet/lipgloss"

var (
	Box = lg.NewStyle().
		Border(lg.RoundedBorder()).
		BorderForeground(lg.Color("#874BFD")).
		Padding(1, 2).
		Align(lg.Center)

	Title = lg.NewStyle().
		Bold(true).
		Foreground(lg.Color("#FAFAFA")).
		Background(lg.Color("#874BFD")).
		Padding(0, 1).
		MarginBottom(1)

	ActionsText = lg.NewStyle().
			Foreground(lg.Color("#6C757D")).
			PaddingRight(3)
)
