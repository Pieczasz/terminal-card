package styles

import (
	lg "github.com/charmbracelet/lipgloss"
	"strings"
)

func RenderMainBox(width, height int, content string) string {
	boxWidth := width * 5 / 6
	boxHeight := height * 5 / 7

	// 4 for horizontal borders/padding, 4 for vertical borders/padding
	placedContent := lg.Place(boxWidth-6, boxHeight-4, lg.Center, lg.Center, content)
	return Box.Width(boxWidth).Height(boxHeight).Render(placedContent)
}

func RenderMainLayout(width, height int, header, content, footer string) string {
	boxWidth := width * 5 / 6
	boxHeight := height * 5 / 7
	
	innerWidth := boxWidth - 6
	innerHeight := boxHeight - 4

	// Strip trailing newlines from go-figure which throw off calculations
	header = strings.TrimRight(header, "\r\n")
	footer = strings.TrimRight(footer, "\r\n")

	hHeader := lg.Height(header)
	hFooter := lg.Height(footer)
	
	hContent := innerHeight - hHeader - hFooter
	
	// Optical centering: by adding two invisible lines to the bottom of the content block, 
	// lipgloss's math pushes the visible text exactly 1 line UP, which feels more natural to the eye.
	content = strings.TrimRight(content, "\r\n") + "\n\n"
	
	headerArea := lg.Place(innerWidth, hHeader, lg.Center, lg.Top, header)
	footerArea := lg.Place(innerWidth, hFooter, lg.Center, lg.Bottom, footer)
	contentArea := lg.Place(innerWidth, hContent, lg.Center, lg.Center, content)
	
	stacked := lg.JoinVertical(lg.Center, headerArea, contentArea, footerArea)
	return Box.Width(boxWidth).Height(boxHeight).Render(stacked)
}

func RenderActionFooter(actions []string) string {
	var renderedActions []string
	for _, action := range actions {
		renderedActions = append(renderedActions, ActionsText.Render(action))
	}
	return strings.Join(renderedActions, "   |   ")
}

var GlobalActions = []string{
	"n - New Game",
	"f - Find Game",
	"p - Profile",
	"q - Quit",
}

var (
	Box = lg.NewStyle().
		Border(lg.RoundedBorder()).
		BorderForeground(lg.Color("#FFFFFF")).
		Padding(1, 2).
		Align(lg.Center)

	Title = lg.NewStyle().
		Bold(true).
		Foreground(lg.Color("#FAFAFA"))

	ActionsText = lg.NewStyle().
			Foreground(lg.Color("#B0B0B0"))

	Welcome = lg.NewStyle().
		Bold(true).
		Foreground(lg.Color("#00FFFF"))
)
