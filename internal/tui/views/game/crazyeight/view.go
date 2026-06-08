package crazyeight

import (
	"terminalcard/internal/deck"
	"terminalcard/internal/game"
	"terminalcard/internal/tui/components"
	gameview "terminalcard/internal/tui/views/game"

	lg "github.com/charmbracelet/lipgloss"
)

func (m Model) View() string {
	if m.baseState.Phase != game.Playing {
		return gameview.RenderWaitingScreen(m.global.Width, m.global.Height, m.baseState.Phase, m.baseState.Winner)
	}

	boardView := m.renderBoard()
	mySection := m.renderPlayerSection()

	content := lg.JoinVertical(lg.Center, boardView, mySection)
	helperText := "←/h: left | →/k: right | enter: play/confirm | d: draw | esc: leave/cancel"

	return gameview.RenderGameScreen(m.global.Width, m.global.Height, content, helperText)
}

func (m Model) renderBoard() string {
	topLayer := m.renderTopOpponent()
	middleLayer := m.renderMiddleLayer()
	return lg.JoinVertical(lg.Center, topLayer, middleLayer)
}

func (m Model) renderMiddleLayer() string {
	leftOpponent := m.renderLeftOpponent()
	rightOpponent := m.renderRightOpponent()
	centerStack := m.renderCenterTable()

	return lg.JoinHorizontal(lg.Center,
		lg.NewStyle().Width(20).Align(lg.Center).Render(leftOpponent),
		lg.NewStyle().Width(30).Align(lg.Center).Render(centerStack),
		lg.NewStyle().Width(20).Align(lg.Center).Render(rightOpponent),
	)
}

func (m Model) renderTopOpponent() string {
	if len(m.baseState.Opponents) == 1 || len(m.baseState.Opponents) >= 3 {
		idx := 0
		if len(m.baseState.Opponents) >= 3 {
			idx = 1
		}
		o := m.baseState.Opponents[idx]
		isTurn := m.baseState.CurrentPlayer == o.ID
		rendered := gameview.RenderOpponent(o, isTurn)
		return lg.NewStyle().Width(70).Height(4).Align(lg.Center).Render(rendered)
	}
	return lg.NewStyle().Width(70).Height(4).Render("")
}

func (m Model) renderLeftOpponent() string {
	if len(m.baseState.Opponents) >= 2 {
		o := m.baseState.Opponents[0]
		isTurn := m.baseState.CurrentPlayer == o.ID
		return gameview.RenderOpponent(o, isTurn)
	}
	return ""
}

func (m Model) renderRightOpponent() string {
	if len(m.baseState.Opponents) == 2 {
		o := m.baseState.Opponents[1]
		isTurn := m.baseState.CurrentPlayer == o.ID
		return gameview.RenderOpponent(o, isTurn)
	} else if len(m.baseState.Opponents) >= 3 {
		o := m.baseState.Opponents[2]
		isTurn := m.baseState.CurrentPlayer == o.ID
		return gameview.RenderOpponent(o, isTurn)
	}
	return ""
}

func (m Model) renderCenterTable() string {
	discardView := components.RenderCard(m.baseState.TopDiscard, false)
	currentSuitView := m.renderCurrentSuitIndicator()
	return lg.JoinVertical(lg.Center, discardView, currentSuitView)
}

func (m Model) renderCurrentSuitIndicator() string {
	suitStr := ""
	switch m.currentSuit {
	case deck.Spades:
		suitStr = "♠ Spades"
	case deck.Hearts:
		suitStr = "♥︎ Hearts"
	case deck.Diamonds:
		suitStr = "♦ Diamonds"
	case deck.Clubs:
		suitStr = "♣ Clubs"
	}

	if suitStr == "" {
		return ""
	}

	return lg.NewStyle().Foreground(lg.Color("#AAAAAA")).Render("Current Suit: ") +
		lg.NewStyle().Bold(true).Render(suitStr)
}

func (m Model) renderPlayerSection() string {
	suitPickerView := m.renderSuitPicker()
	statusView := gameview.RenderStatus(m.baseState.CurrentPlayer, m.baseState.MyTurn)
	handView := gameview.RenderHand(m.baseState.Hand, m.selectedCardIdx, m.pickingSuit)

	return lg.JoinVertical(lg.Center,
		suitPickerView,
		statusView,
		handView,
	)
}

func (m Model) renderSuitPicker() string {
	if !m.pickingSuit {
		return ""
	}

	suits := []string{"♠ Spades", "♥︎ Hearts", "♦ Diamonds", "♣ Clubs"}
	var renderedSuits []string

	for i, suitName := range suits {
		style := lg.NewStyle().Padding(0, 1).Border(lg.RoundedBorder())
		if i == m.suitCursor {
			style = style.BorderForeground(lg.Color("205")).Foreground(lg.Color("205"))
		} else {
			style = style.BorderForeground(lg.Color("#555555"))
		}
		renderedSuits = append(renderedSuits, style.Render(suitName))
	}

	pickerBox := lg.JoinHorizontal(lg.Center, renderedSuits...)
	return lg.JoinVertical(lg.Center,
		lg.NewStyle().Bold(true).Render("Pick a suit:"),
		pickerBox,
	)
}
