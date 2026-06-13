package game

import (
	"fmt"
	"strings"
	"terminalcard/internal/deck"
	"terminalcard/internal/game"
	"terminalcard/internal/tui/components"
	"terminalcard/internal/tui/styles"
	"terminalcard/internal/tui/views"

	"math"

	lg "charm.land/lipgloss/v2"
)

func RenderHand(hand []deck.Card, selectedIdx int, selectionLift float64, disableSelection bool) string {
	var renderedCards []string
	for i, c := range hand {
		isSelected := i == selectedIdx && !disableSelection
		var lift int
		if isSelected {
			lift = int(math.Round(selectionLift))
			if lift < 0 {
				lift = 0
			}
		}

		cardView := components.RenderCard(c, isSelected)
		if i < 10 {
			numStyle := lg.NewStyle().Foreground(lg.Color("#888888"))
			if isSelected {
				numStyle = numStyle.Foreground(lg.Color("205")).Bold(true)
			}
			numView := numStyle.Render(fmt.Sprintf("%d", i))
			cardView = lg.JoinVertical(lg.Center, cardView, numView)
		}

		maxLift := 2
		if lift > maxLift {
			lift = maxLift
		}

		cardView = lg.NewStyle().
			MarginTop(maxLift - lift).
			MarginBottom(lift).
			Render(cardView)

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

	cardColor := lg.Color("#EEEEEE")
	textColor := lg.Color("#AAAAAA")

	botLine := lg.NewStyle().Foreground(cardColor).Render("╰" + strings.Repeat("┴", count-1) + "───────╯")

	edge := lg.NewStyle().Foreground(cardColor).Render("│" + strings.Repeat("│", count-1))
	body := lg.NewStyle().Foreground(textColor).Render("░░░░░░░")
	rightEdge := lg.NewStyle().Foreground(cardColor).Render("│")

	midLine := edge + body + rightEdge

	var sb strings.Builder
	sb.Grow(len(midLine)*4 + len(botLine) + 5)

	sb.WriteString(midLine)
	sb.WriteByte('\n')
	sb.WriteString(midLine)
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

	cardColor := lg.Color("#EEEEEE")
	textColor := lg.Color("#AAAAAA")

	topEdge := lg.NewStyle().Foreground(cardColor).Render("─────╮")
	midEdge := lg.NewStyle().Foreground(cardColor).Render("─────┤")
	botEdge := lg.NewStyle().Foreground(cardColor).Render("─────╯")
	cardBody := lg.NewStyle().Foreground(textColor).Render("░░░░░") + lg.NewStyle().Foreground(cardColor).Render("│")

	return buildVerticalCardsString(count, topEdge, midEdge, botEdge, cardBody)
}

func renderRightCards(count int) string {
	if count <= 0 {
		return ""
	}

	cardColor := lg.Color("#EEEEEE")
	textColor := lg.Color("#AAAAAA")

	topEdge := lg.NewStyle().Foreground(cardColor).Render("╭─────")
	midEdge := lg.NewStyle().Foreground(cardColor).Render("├─────")
	botEdge := lg.NewStyle().Foreground(cardColor).Render("╰─────")
	cardBody := lg.NewStyle().Foreground(cardColor).Render("│") + lg.NewStyle().Foreground(textColor).Render("░░░░░")

	return buildVerticalCardsString(count, topEdge, midEdge, botEdge, cardBody)
}

func buildVerticalCardsString(count int, topEdge, midEdge, botEdge, cardBody string) string {
	var sb strings.Builder
	sb.Grow((count + 3) * 20)

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

	switch orientation {
	case OrientationTop:
		// Below the deck on top
		return lg.JoinVertical(lg.Center, cardsView, infoView)
	case OrientationLeft:
		// Above the deck on left and right side
		return lg.JoinVertical(lg.Left, infoView, cardsView)
	default:
		return lg.JoinVertical(lg.Right, infoView, cardsView)
	}
}

func RenderOpponentMinimal(o game.PlayerSnapshot, isCurrentTurn bool) string {
	nameStyle := lg.NewStyle().Foreground(lg.Color("#FFA500")).Bold(true)
	if isCurrentTurn {
		nameStyle = nameStyle.Background(lg.Color("#555555")).Padding(0, 1)
	}
	nameView := nameStyle.Render(o.ID)
	cardsCountView := lg.NewStyle().Foreground(lg.Color("#AAAAAA")).Render(fmt.Sprintf("[%d cards]", o.HandSize))

	return lg.JoinHorizontal(lg.Center, nameView, " ", cardsCountView)
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

func RenderWaitingScreen(width, height int, phase game.Phase, winner string) string {
	innerWidth := styles.GetInnerWidth(width)
	titleFig := styles.RenderFigureASCII("Active Game", innerWidth)
	titleText := styles.Title.Render(titleFig)
	header := styles.Title.Render(titleText)
	footer := lg.NewStyle().Render(styles.RenderActionFooter(styles.GlobalActions))

	var content string
	if phase == game.Finished {
		content = fmt.Sprintf("Game Over! Winner: %s\n\nPress Esc to go back.", winner)
	} else {
		content = "Waiting for game to start..."
	}

	return views.RenderCenteredLayout(width, height, header, content, footer)
}
