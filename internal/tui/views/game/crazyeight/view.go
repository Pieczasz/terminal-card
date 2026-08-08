package crazyeight

import (
	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/tui/components"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"
	gameview "github.com/Pieczasz/terminal-card/internal/tui/views/game"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
)

const keyHints = "<-/h: left | ->/l: right | enter: play/confirm | d: draw | esc: leave/cancel"

// suitLabels are the picker cells, in grid order. suitPickerOrder in update.go
// maps a cursor position back to the suit and must stay in the same order.
var suitLabels = []string{"♠ Spades", "♥︎ Hearts", "♦ Diamonds", "♣ Clubs"}

// suitCellWidth keeps the 2x2 picker a rectangle whatever the suit name length.
// lipgloss counts the border and padding inside Width, so the widest label needs
// four extra columns - without them "♦ Diamonds" wraps under its own glyph.
var suitCellWidth = widestLabel(suitLabels) + 4

func widestLabel(labels []string) int {
	widest := 0
	for _, l := range labels {
		widest = max(widest, lg.Width(l))
	}
	return widest
}

func (m *Model) View() tea.View {
	if m.baseState.Phase != game.Playing {
		return tea.NewView(gameview.RenderWaitingScreen(m.global, m.baseState.Phase, m.baseState.Winner))
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
		fullPlayerArea = lg.JoinVertical(lg.Center, mySection, m.global.Theme.Dim.Render(keyHints))
	} else {
		hints := m.global.Theme.Dim.MarginTop(1).Render(keyHints)
		fullPlayerArea = lg.NewStyle().MarginBottom(1).Render(lg.JoinVertical(lg.Center, mySection, hints))
	}

	topHeight := lg.Height(topAreaContent)
	botHeight := lg.Height(fullPlayerArea)
	midHeight := max(m.global.Height-topHeight-botHeight, 0)

	topArea := styles.PadCenter(m.global.Width, topAreaContent)
	midArea := m.renderMiddleLayer(midHeight, superCompact)
	botArea := styles.PadCenter(m.global.Width, fullPlayerArea)

	return tea.NewView(lg.JoinVertical(lg.Left, topArea, midArea, botArea))
}

func (m *Model) renderMiddleLayer(height int, superCompact bool) string {
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

	leftArea := styles.Place(w1, height, lg.Left, lg.Center, leftOpponent)
	centerArea := styles.Place(w2, height, lg.Center, lg.Center, lg.NewStyle().MarginTop(1).Render(centerStack))
	rightArea := styles.Place(w3, height, lg.Right, lg.Center, rightOpponent)

	return lg.JoinHorizontal(lg.Top, leftArea, centerArea, rightArea)
}

func (m *Model) renderTopOpponent(superCompact bool) string {
	if len(m.baseState.Opponents) == 1 || len(m.baseState.Opponents) >= 3 {
		idx := 0
		if len(m.baseState.Opponents) >= 3 {
			idx = 1
		}
		o := m.baseState.Opponents[idx]
		isTurn := m.baseState.CurrentPlayer == o.Username
		if superCompact {
			return gameview.RenderOpponentMinimal(m.global.Theme, o, isTurn)
		}
		return gameview.RenderOpponent(m.global.Theme, o, isTurn, gameview.OrientationTop, m.baseState.TurnRemaining)
	}
	return ""
}

func (m *Model) renderLeftOpponent(superCompact bool) string {
	if len(m.baseState.Opponents) >= 2 {
		o := m.baseState.Opponents[0]
		isTurn := m.baseState.CurrentPlayer == o.Username
		if superCompact {
			return gameview.RenderOpponentMinimal(m.global.Theme, o, isTurn)
		}
		return gameview.RenderOpponent(m.global.Theme, o, isTurn, gameview.OrientationLeft, m.baseState.TurnRemaining)
	}
	return ""
}

func (m *Model) renderRightOpponent(superCompact bool) string {
	if len(m.baseState.Opponents) == 2 {
		o := m.baseState.Opponents[1]
		isTurn := m.baseState.CurrentPlayer == o.Username
		if superCompact {
			return gameview.RenderOpponentMinimal(m.global.Theme, o, isTurn)
		}
		return gameview.RenderOpponent(m.global.Theme, o, isTurn, gameview.OrientationRight, m.baseState.TurnRemaining)
	} else if len(m.baseState.Opponents) >= 3 {
		o := m.baseState.Opponents[2]
		isTurn := m.baseState.CurrentPlayer == o.Username
		if superCompact {
			return gameview.RenderOpponentMinimal(m.global.Theme, o, isTurn)
		}
		return gameview.RenderOpponent(m.global.Theme, o, isTurn, gameview.OrientationRight, m.baseState.TurnRemaining)
	}
	return ""
}

func (m *Model) renderCenterTable() string {
	discardView := components.RenderCard(m.global.Theme, m.baseState.TopDiscard, false)
	currentSuitView := m.renderCurrentSuitIndicator()
	return lg.JoinVertical(lg.Center, discardView, currentSuitView)
}

func (m *Model) renderCurrentSuitIndicator() string {
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

	return m.global.Theme.Muted.Render("Current Suit: ") +
		lg.NewStyle().Bold(true).Foreground(m.global.Theme.Text).Render(suitStr)
}

func (m *Model) renderPlayerSection() string {
	statusView := gameview.RenderStatus(m.global.Theme, m.baseState.CurrentPlayer, m.baseState.MyTurn)
	handView := gameview.RenderHand(m.global.Theme, m.baseState.Hand, m.selectedCardIdx, m.pickingSuit)

	sections := []string{statusView, handView}
	// The hero's own clock goes under their hand, where every other seat's is.
	if m.baseState.MyTurn {
		if clock := gameview.RenderTurnClock(m.global.Theme, m.baseState.TurnRemaining, true); clock != "" {
			sections = append(sections, clock)
		}
	}
	if m.lastActionErr != nil {
		errView := m.global.Theme.ErrorText.Render(m.lastActionErr.Error())
		sections = append(sections, errView)
	}

	return lg.JoinVertical(lg.Center, sections...)
}

func (m *Model) renderSuitPicker() string {
	if !m.pickingSuit {
		return ""
	}

	t := m.global.Theme
	renderedSuits := make([]string, 0, len(suitLabels))
	for i, suitName := range suitLabels {
		style := lg.NewStyle().Padding(0, 1).Border(lg.RoundedBorder())
		if i == m.suitCursor {
			style = style.BorderForeground(t.Selection).Foreground(t.Selection).Bold(true)
		} else {
			style = style.BorderForeground(t.BorderMuted).Foreground(t.TextMuted)
		}
		renderedSuits = append(renderedSuits, style.Width(suitCellWidth).Align(lg.Center).Render(suitName))
	}

	row1 := lg.JoinHorizontal(lg.Center, renderedSuits[0], renderedSuits[1])
	row2 := lg.JoinHorizontal(lg.Center, renderedSuits[2], renderedSuits[3])
	pickerBox := lg.JoinVertical(lg.Center, row1, row2)

	return lg.NewStyle().
		Border(lg.RoundedBorder()).
		BorderForeground(t.Selection).
		Padding(1, 2).
		Render(
			lg.JoinVertical(lg.Center,
				lg.NewStyle().Bold(true).Foreground(t.Selection).Render("Pick a suit:"),
				"",
				pickerBox,
			),
		)
}
