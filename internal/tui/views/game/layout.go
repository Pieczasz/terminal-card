package game

import (
	"fmt"
	"strings"
	"terminalcard/internal/deck"
	"terminalcard/internal/game"
	"terminalcard/internal/tui/components"
	"terminalcard/internal/tui/styles"
	"terminalcard/internal/tui/views/common"

	lg "github.com/charmbracelet/lipgloss"
)

func RenderHand(hand []deck.Card, selectedIdx int, disableSelection bool) string {
	var renderedCards []string
	for i, c := range hand {
		isSelected := i == selectedIdx && !disableSelection
		cardView := components.RenderCard(c, isSelected)
		if i < 10 {
			numStyle := lg.NewStyle().Foreground(lg.Color("#888888"))
			if isSelected {
				numStyle = numStyle.Foreground(lg.Color("205")).Bold(true)
			}
			numView := numStyle.Render(fmt.Sprintf("%d", i))
			cardView = lg.JoinVertical(lg.Center, cardView, numView)
		}
		renderedCards = append(renderedCards, cardView)
	}
	return lg.JoinHorizontal(lg.Top, renderedCards...)
}

type Orientation int

const (
	OrientationTop Orientation = iota
	OrientationLeft
	OrientationRight
)

func renderTopCards(count int) string {
	if count <= 0 {
		return ""
	}
	
	cardColor := lg.Color("#444444")
	textColor := lg.Color("#AAAAAA")

	topLine := lg.NewStyle().Foreground(cardColor).Render("╭" + strings.Repeat("┬", count-1) + "───────╮")
	botLine := lg.NewStyle().Foreground(cardColor).Render("╰" + strings.Repeat("┴", count-1) + "───────╯")
	
	edge := lg.NewStyle().Foreground(cardColor).Render("│" + strings.Repeat("│", count-1))
	body := lg.NewStyle().Foreground(textColor).Render("░░░░░░░")
	rightEdge := lg.NewStyle().Foreground(cardColor).Render("│")

	midLine := edge + body + rightEdge

	var sb strings.Builder
	sb.Grow(len(topLine) + len(midLine)*2 + len(botLine) + 3)

	sb.WriteString(topLine)
	sb.WriteByte('\n')
	sb.WriteString(midLine)
	sb.WriteByte('\n')
	sb.WriteString(midLine)
	sb.WriteByte('\n')
	sb.WriteString(botLine)

	return sb.String()
}

func renderLeftCards(count int) string {
	if count <= 0 {
		return ""
	}
	
	cardColor := lg.Color("#444444")
	textColor := lg.Color("#AAAAAA")

	topEdge := lg.NewStyle().Foreground(cardColor).Render("─────╮")
	midEdge := lg.NewStyle().Foreground(cardColor).Render("─────┤")
	botEdge := lg.NewStyle().Foreground(cardColor).Render("─────╯")
	cardBody := lg.NewStyle().Foreground(textColor).Render("░░░░░") + lg.NewStyle().Foreground(cardColor).Render("│")

	var sb strings.Builder
	sb.Grow((count+3) * 20) // Pre-allocate approximate capacity

	sb.WriteString(topEdge)
	sb.WriteByte('\n')
	for i := 0; i < count-1; i++ {
		sb.WriteString(midEdge)
		sb.WriteByte('\n')
	}
	sb.WriteString(cardBody)
	sb.WriteByte('\n')
	sb.WriteString(cardBody)
	sb.WriteByte('\n')
	sb.WriteString(botEdge)

	return sb.String()
}

func renderRightCards(count int) string {
	if count <= 0 {
		return ""
	}
	
	cardColor := lg.Color("#444444")
	textColor := lg.Color("#AAAAAA")

	topEdge := lg.NewStyle().Foreground(cardColor).Render("╭─────")
	midEdge := lg.NewStyle().Foreground(cardColor).Render("├─────")
	botEdge := lg.NewStyle().Foreground(cardColor).Render("╰─────")
	cardBody := lg.NewStyle().Foreground(cardColor).Render("│") + lg.NewStyle().Foreground(textColor).Render("░░░░░")

	var sb strings.Builder
	sb.Grow((count+3) * 20)

	sb.WriteString(topEdge)
	sb.WriteByte('\n')
	for i := 0; i < count-1; i++ {
		sb.WriteString(midEdge)
		sb.WriteByte('\n')
	}
	sb.WriteString(cardBody)
	sb.WriteByte('\n')
	sb.WriteString(cardBody)
	sb.WriteByte('\n')
	sb.WriteString(botEdge)

	return sb.String()
}

func RenderOpponent(o game.PlayerSnapshot, isCurrentTurn bool, orientation Orientation) string {
	nameStyle := lg.NewStyle().Foreground(lg.Color("#FFA500")).Bold(true)
	if isCurrentTurn {
		nameStyle = nameStyle.Background(lg.Color("#555555")).Padding(0, 1)
	}
	nameView := nameStyle.Render(o.ID)
	cardsCountView := lg.NewStyle().Foreground(lg.Color("#AAAAAA")).Render(fmt.Sprintf("[%d cards]", o.HandSize))

	infoView := lg.JoinVertical(lg.Center, nameView, cardsCountView)

	var cardsView string
	switch orientation {
	case OrientationTop:
		cardsView = renderTopCards(o.HandSize)
	case OrientationLeft:
		cardsView = renderLeftCards(o.HandSize)
	case OrientationRight:
		cardsView = renderRightCards(o.HandSize)
	}

	if orientation == OrientationTop {
		// Below the deck on top
		return lg.JoinVertical(lg.Center, cardsView, infoView)
	} else {
		// Above the deck on left and right side
		return lg.JoinVertical(lg.Center, infoView, cardsView)
	}
}

func RenderStatus(currentPlayer string, isMyTurn bool) string {
	statusStyle := lg.NewStyle().Foreground(lg.Color("#888888")).MarginTop(1).MarginBottom(1)
	statusStr := fmt.Sprintf("Current turn: %s", currentPlayer)
	if isMyTurn {
		statusStyle = statusStyle.Foreground(lg.Color("#00FF00")).Bold(true)
		statusStr = "> YOUR TURN <"
	}
	return statusStyle.Render(statusStr)
}

func RenderGameScreen(width, height int, content string, helperText string) string {
	helpers := lg.NewStyle().Foreground(lg.Color("#888888")).MarginTop(1).Render(helperText)
	fullContent := lg.JoinVertical(lg.Center, content, helpers)

	return lg.Place(
		width, height,
		lg.Center, lg.Center,
		fullContent,
	)
}

func RenderWaitingScreen(width, height int, phase game.Phase, winner string) string {
	innerWidth := styles.GetInnerWidth(width)
	titleFig := styles.RenderFigureAscii("Active Game", innerWidth)
	titleText := styles.Title.Render(titleFig)
	header := styles.Title.Render(titleText)
	footer := lg.NewStyle().Render(styles.RenderActionFooter(styles.GlobalActions))

	var content string
	if phase == game.Finished {
		content = fmt.Sprintf("Game Over! Winner: %s\n\nPress Esc to go back.", winner)
	} else {
		content = "Waiting for game to start..."
	}

	return common.RenderCenteredLayout(width, height, header, content, footer)
}
