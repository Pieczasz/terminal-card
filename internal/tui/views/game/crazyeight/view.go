package crazyeight

import (
	"terminalcard/internal/deck"
	"terminalcard/internal/game"
	"terminalcard/internal/tui/components"
	gameview "terminalcard/internal/tui/views/game"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
)

func (m Model) View() tea.View {
	if m.baseState.Phase != game.Playing {
		return tea.NewView(gameview.RenderWaitingScreen(m.global.Width, m.global.Height, m.baseState.Phase, m.baseState.Winner))
	}

	compactMode := m.global.Height < 30
	superCompact := m.global.Height < 24

	topSection := m.renderTopOpponent(superCompact)
	var topAreaContent string
	if superCompact {
		topAreaContent = topSection
	} else {
		topAreaContent = lg.NewStyle().MarginTop(1).Render(topSection)
	}

	mySection := m.renderPlayerSection()
	var fullPlayerArea string
	if superCompact {
		fullPlayerArea = mySection
	} else if compactMode {
		helperText := lg.NewStyle().Foreground(lg.Color("#888888")).Render("←/h: left | →/k: right | enter: play/confirm | d: draw | esc: leave/cancel")
		fullPlayerArea = lg.JoinVertical(lg.Center, mySection, helperText)
	} else {
		helperText := lg.NewStyle().Foreground(lg.Color("#888888")).MarginTop(1).Render("←/h: left | →/k: right | enter: play/confirm | d: draw | esc: leave/cancel")
		fullPlayerArea = lg.NewStyle().MarginBottom(1).Render(lg.JoinVertical(lg.Center, mySection, helperText))
	}

	topHeight := lg.Height(topAreaContent)
	botHeight := lg.Height(fullPlayerArea)
	midHeight := max(m.global.Height-topHeight-botHeight, 0)

	topArea := lg.PlaceHorizontal(m.global.Width, lg.Center, topAreaContent)
	midArea := m.renderMiddleLayer(midHeight, superCompact)
	botArea := lg.PlaceHorizontal(m.global.Width, lg.Center, fullPlayerArea)

	return tea.NewView(lg.JoinVertical(lg.Left, topArea, midArea, botArea))
}

func (m Model) renderMiddleLayer(height int, superCompact bool) string {
	leftOpponent := m.renderLeftOpponent(superCompact)
	rightOpponent := m.renderRightOpponent(superCompact)

	var centerStack string
	if m.pickingSuit {
		centerStack = m.renderSuitPicker()
	} else {
		centerStack = m.renderCenterTable()
	}

	w1 := m.global.Width / 3
	w2 := m.global.Width / 3
	w3 := m.global.Width - w1 - w2

	leftArea := lg.Place(w1, height, lg.Left, lg.Center, leftOpponent)
	centerArea := lg.Place(w2, height, lg.Center, lg.Center, lg.NewStyle().MarginTop(1).Render(centerStack))
	rightArea := lg.Place(w3, height, lg.Right, lg.Center, rightOpponent)

	return lg.JoinHorizontal(lg.Top, leftArea, centerArea, rightArea)
}

func (m Model) renderTopOpponent(superCompact bool) string {
	if len(m.baseState.Opponents) == 1 || len(m.baseState.Opponents) >= 3 {
		idx := 0
		if len(m.baseState.Opponents) >= 3 {
			idx = 1
		}
		o := m.baseState.Opponents[idx]
		isTurn := m.baseState.CurrentPlayer == o.ID
		if superCompact {
			return gameview.RenderOpponentMinimal(o, isTurn)
		}
		return gameview.RenderOpponent(o, isTurn, gameview.OrientationTop)
	}
	return ""
}

func (m Model) renderLeftOpponent(superCompact bool) string {
	if len(m.baseState.Opponents) >= 2 {
		o := m.baseState.Opponents[0]
		isTurn := m.baseState.CurrentPlayer == o.ID
		if superCompact {
			return gameview.RenderOpponentMinimal(o, isTurn)
		}
		return gameview.RenderOpponent(o, isTurn, gameview.OrientationLeft)
	}
	return ""
}

func (m Model) renderRightOpponent(superCompact bool) string {
	if len(m.baseState.Opponents) == 2 {
		o := m.baseState.Opponents[1]
		isTurn := m.baseState.CurrentPlayer == o.ID
		if superCompact {
			return gameview.RenderOpponentMinimal(o, isTurn)
		}
		return gameview.RenderOpponent(o, isTurn, gameview.OrientationRight)
	} else if len(m.baseState.Opponents) >= 3 {
		o := m.baseState.Opponents[2]
		isTurn := m.baseState.CurrentPlayer == o.ID
		if superCompact {
			return gameview.RenderOpponentMinimal(o, isTurn)
		}
		return gameview.RenderOpponent(o, isTurn, gameview.OrientationRight)
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
	case deck.NoSuit:
		suitStr = ""
	}

	if suitStr == "" {
		return ""
	}

	return lg.NewStyle().Foreground(lg.Color("#AAAAAA")).Render("Current Suit: ") +
		lg.NewStyle().Bold(true).Render(suitStr)
}

func (m Model) renderPlayerSection() string {
	statusView := gameview.RenderStatus(m.baseState.CurrentPlayer, m.baseState.MyTurn)
	handView := gameview.RenderHand(m.baseState.Hand, m.selectedCardIdx, m.selectionLift, m.pickingSuit)

	return lg.JoinVertical(lg.Center,
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
			style = style.BorderForeground(lg.Color("205")).Foreground(lg.Color("205")).Bold(true)
		} else {
			style = style.BorderForeground(lg.Color("#555555")).Foreground(lg.Color("#AAAAAA"))
		}
		// ensure uniform width for grid
		renderedSuits = append(renderedSuits, style.Width(12).Align(lg.Center).Render(suitName))
	}

	row1 := lg.JoinHorizontal(lg.Center, renderedSuits[0], renderedSuits[1])
	row2 := lg.JoinHorizontal(lg.Center, renderedSuits[2], renderedSuits[3])
	pickerBox := lg.JoinVertical(lg.Center, row1, row2)

	return lg.NewStyle().
		Border(lg.RoundedBorder()).
		BorderForeground(lg.Color("205")).
		Padding(1, 2).
		Render(
			lg.JoinVertical(lg.Center,
				lg.NewStyle().Bold(true).Foreground(lg.Color("205")).Render("Pick a suit:"),
				"",
				pickerBox,
			),
		)
}
