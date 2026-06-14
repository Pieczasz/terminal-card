package styles

import (
	"strings"

	lg "charm.land/lipgloss/v2"
	"github.com/common-nighthawk/go-figure"
)

func GetBoxWidth(screenWidth int) int {
	if screenWidth < 100 {
		return screenWidth - 4
	}
	return screenWidth * 5 / 6
}

func GetBoxHeight(screenHeight int) int {
	if screenHeight < 30 {
		return screenHeight - 2
	}
	return screenHeight * 5 / 7
}

func GetInnerWidth(screenWidth int) int {
	return GetBoxWidth(screenWidth) - 6
}

func GetAvailableContentHeight(screenHeight int, header, footer string) int {
	boxHeight := GetBoxHeight(screenHeight)
	innerHeight := boxHeight - 4

	header = strings.TrimRight(header, "\r\n")
	footer = strings.TrimRight(footer, "\r\n")

	return innerHeight - lg.Height(header) - lg.Height(footer)
}

func GetAvailableContentWidth(screenWidth int) int {
	return GetBoxWidth(screenWidth) - 6
}

func RenderFigureASCII(text string, maxWidth int) string {
	fonts := []string{"slant", "small", "mini"}
	for _, font := range fonts {
		fig := figure.NewFigure(text, font, true).String()
		if lg.Width(fig) <= maxWidth {
			return fig
		}
	}
	// Fallback to pure string if even mini is too large
	return text
}

func RenderMainLayout(width, height int, header, content, footer string) string {
	boxWidth := GetBoxWidth(width)
	boxHeight := GetBoxHeight(height)

	innerWidth := boxWidth - 6
	innerHeight := boxHeight - 4

	// Strip trailing newlines from go-figure which throw off calculations (for centering)
	header = strings.TrimRight(header, "\r\n")
	footer = strings.TrimRight(footer, "\r\n")

	// Pre-wrap header and footer so their heights are accurately calculated
	header = lg.NewStyle().Width(innerWidth).Align(lg.Center).Render(header)
	footer = lg.NewStyle().Width(innerWidth).Align(lg.Center).Render(footer)

	hHeader := lg.Height(header)
	hFooter := lg.Height(footer)

	hContent := max(innerHeight-hHeader-hFooter, 0)

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
	// Use a compact pipe separator
	return strings.Join(renderedActions, " | ")
}

var GlobalActions = []string{"n - New Game", "f - Join Game", "p - Profile", "t - Leaderboard", "ctrl+c - Quit"}

var (
	Box = lg.NewStyle().
		Border(lg.RoundedBorder()).
		BorderForeground(lg.Color("#FFFFFF")).
		Padding(1, 2)

	Title = lg.NewStyle().
		Bold(true).
		Foreground(lg.Color("#FAFAFA"))

	ActionsText = lg.NewStyle().
			Foreground(lg.Color("#B0B0B0"))

	Welcome = lg.NewStyle().
		Bold(true).
		Foreground(lg.Color("#00FFFF"))

	LobbyCode = lg.NewStyle().
			Foreground(lg.Color("#FFA500")).
			Bold(true)

	PlayerItemSelected = lg.NewStyle().
				Foreground(lg.Color("205"))

	HostTag = lg.NewStyle().
		Foreground(lg.Color("#FFD700")).
		Bold(true)

	GuestTag = lg.NewStyle().
			Foreground(lg.Color("#A9A9A9"))
)
