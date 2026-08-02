package styles

import (
	"strings"

	lg "charm.land/lipgloss/v2"
	"github.com/common-nighthawk/go-figure"
)

// Box dimensions stop growing past these so content stays readable on an ultra-wide
// or very tall terminal instead of stretching across the whole screen.
const (
	maxBoxWidth  = 120
	maxBoxHeight = 40
)

// BoxWidth is the outer width of the framed layout. It must never shrink as the
// terminal grows, and must never go negative: Router.Global.Width is 0 until the
// first WindowSizeMsg, so every session's opening frame renders at zero.
func BoxWidth(screenWidth int) int {
	return max(min(screenWidth-4, maxBoxWidth), 0)
}

func BoxHeight(screenHeight int) int {
	return max(min(screenHeight-2, maxBoxHeight), 0)
}

func InnerWidth(screenWidth int) int {
	return max(BoxWidth(screenWidth)-6, 0)
}

func AvailableContentHeight(screenHeight int, header, footer string) int {
	boxHeight := BoxHeight(screenHeight)
	innerHeight := boxHeight - 4

	header = strings.TrimRight(header, "\r\n")
	footer = strings.TrimRight(footer, "\r\n")

	return max(innerHeight-lg.Height(header)-lg.Height(footer), 0)
}

func AvailableContentWidth(screenWidth int) int {
	return max(BoxWidth(screenWidth)-6, 0)
}

func RenderFigureASCII(text string, maxWidth int) string {
	fonts := []string{"slant", "small", "mini"}
	for _, font := range fonts {
		fig := figure.NewFigure(text, font, true).String()
		if lg.Width(fig) <= maxWidth {
			return fig
		}
	}
	return text // no font fits; plain text always does
}

func RenderMainLayout(width, height int, header, content, footer string) string {
	boxWidth := BoxWidth(width)
	boxHeight := BoxHeight(height)

	innerWidth := boxWidth - 6
	innerHeight := boxHeight - 4

	// go-figure leaves trailing newlines that inflate the measured height below.
	header = strings.TrimRight(header, "\r\n")
	footer = strings.TrimRight(footer, "\r\n")

	// Wrap before measuring, or lg.Height reports the unwrapped height.
	header = lg.NewStyle().Width(innerWidth).Align(lg.Center).Render(header)
	footer = lg.NewStyle().Width(innerWidth).Align(lg.Center).Render(footer)

	hHeader := lg.Height(header)
	hFooter := lg.Height(footer)

	hContent := max(innerHeight-hHeader-hFooter, 0)

	// Optical centering: two trailing blank lines push the visible text one line up,
	// which reads as centered where true centering reads as slightly low.
	content = strings.TrimRight(content, "\r\n") + "\n\n"

	headerArea := lg.Place(innerWidth, hHeader, lg.Center, lg.Top, header)
	footerArea := lg.Place(innerWidth, hFooter, lg.Center, lg.Bottom, footer)
	contentArea := lg.Place(innerWidth, hContent, lg.Center, lg.Center, content)

	stacked := lg.JoinVertical(lg.Center, headerArea, contentArea, footerArea)
	return Box.Width(boxWidth).Height(boxHeight).Render(stacked)
}

func RenderActionFooter(actions []string) string {
	renderedActions := make([]string, 0, len(actions))
	for _, action := range actions {
		renderedActions = append(renderedActions, ActionsText.Render(action))
	}
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

	// SectionHeading labels a block of content within a view (Settings, Players,
	// Rankings, Recent Matches, and the player name in the game layout).
	SectionHeading = lg.NewStyle().
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
