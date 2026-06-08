package game

import (
	"fmt"
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

func RenderOpponent(o game.PlayerSnapshot, isCurrentTurn bool) string {
	nameStyle := lg.NewStyle().Foreground(lg.Color("#FFA500")).Bold(true)
	if isCurrentTurn {
		nameStyle = nameStyle.Background(lg.Color("#555555")).Padding(0, 1)
	}
	name := nameStyle.Render(o.ID)
	cards := fmt.Sprintf("[%d cards]", o.HandSize)
	return lg.JoinVertical(lg.Center, name, cards)
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
