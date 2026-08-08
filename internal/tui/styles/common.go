package styles

import (
	"fmt"
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

// The framed layout needs this much terminal to draw without overflowing: the
// poker board alone is five cards at 11 columns each. Below it, views are told to
// render the resize prompt instead of a broken frame - lipgloss will happily wrap
// a table into unreadable confetti rather than complain.
const (
	MinWidth  = 64
	MinHeight = 20
)

// TooSmall reports whether the terminal cannot fit the layout. A zero dimension
// means the first WindowSizeMsg has not arrived yet, which is not the same as
// small - answering true there would flash the resize prompt on every connection.
func TooSmall(screenWidth, screenHeight int) bool {
	if screenWidth <= 0 || screenHeight <= 0 {
		return false
	}
	return screenWidth < MinWidth || screenHeight < MinHeight
}

// RenderTooSmall fills the terminal with the resize prompt. It reports the current
// size as well as the required one, so the player can see which way to drag.
func (t Theme) RenderTooSmall(screenWidth, screenHeight int) string {
	msg := lg.JoinVertical(lg.Center,
		t.Accented.Render("Terminal too small"),
		"",
		t.Muted.Render(fmt.Sprintf("need %d x %d", MinWidth, MinHeight)),
		t.Muted.Render(fmt.Sprintf("have %d x %d", screenWidth, screenHeight)),
	)
	return lg.Place(max(screenWidth, 1), max(screenHeight, 1), lg.Center, lg.Center, msg)
}

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

// PadTruncate fits s into exactly width cells, padding short values and eliding
// long ones. It counts runes rather than bytes so a multi-byte username is never
// cut mid-character, and it is what keeps aligned columns aligned once a player
// uses all 16 characters of their name.
func PadTruncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s + strings.Repeat(" ", width-len(runes))
	}
	if width <= 3 {
		return string(runes[:width])
	}
	return string(runes[:width-3]) + "..."
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

func (t Theme) RenderMainLayout(width, height int, header, content, footer string) string {
	boxWidth := BoxWidth(width)
	boxHeight := BoxHeight(height)

	innerWidth := max(boxWidth-6, 0)
	innerHeight := max(boxHeight-4, 0)

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

	headerArea := Place(innerWidth, hHeader, lg.Center, lg.Top, header)
	footerArea := Place(innerWidth, hFooter, lg.Center, lg.Bottom, footer)
	contentArea := Place(innerWidth, hContent, lg.Center, lg.Center, content)

	stacked := lg.JoinVertical(lg.Center, headerArea, contentArea, footerArea)
	return t.Box.Width(boxWidth).Height(boxHeight).Render(stacked)
}

func (t Theme) RenderActionFooter(actions []string) string {
	renderedActions := make([]string, 0, len(actions))
	for _, action := range actions {
		renderedActions = append(renderedActions, t.ActionsText.Render(action))
	}
	return strings.Join(renderedActions, " | ")
}

var GlobalActions = []string{"n - New Game", "f - Join Game", "p - Profile", "t - Leaderboard", "ctrl+c - Quit"}
