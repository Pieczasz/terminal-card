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
	if m.Base.Phase != game.Playing {
		return tea.NewView(gameview.RenderWaitingScreen(m.Global, m.Base.Phase, m.Base.Winner))
	}

	compactMode := m.Global.Height < 30
	superCompact := m.Global.Height < 24

	opponents := gameview.RenderOpponentEdges(m.Global.Theme, m.Base, superCompact)
	topSection := opponents.Top
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
		fullPlayerArea = lg.JoinVertical(lg.Center, mySection, m.Global.Theme.Dim.Render(keyHints))
	} else {
		hints := m.Global.Theme.Dim.MarginTop(1).Render(keyHints)
		fullPlayerArea = lg.NewStyle().MarginBottom(1).Render(lg.JoinVertical(lg.Center, mySection, hints))
	}

	topHeight := lg.Height(topAreaContent)
	botHeight := lg.Height(fullPlayerArea)
	midHeight := max(m.Global.Height-topHeight-botHeight, 0)

	topArea := styles.PadCenter(m.Global.Width, topAreaContent)
	midArea := m.renderMiddleLayer(midHeight, opponents)
	botArea := styles.PadCenter(m.Global.Width, fullPlayerArea)

	return tea.NewView(lg.JoinVertical(lg.Left, topArea, midArea, botArea))
}

func (m *Model) renderMiddleLayer(height int, opponents gameview.OpponentEdges) string {
	var centerStack string
	if m.pickingSuit {
		centerStack = m.renderSuitPicker()
	} else {
		centerStack = m.renderCenterTable()
	}

	w1 := m.Global.Width / 3
	w2 := m.Global.Width / 3
	w3 := m.Global.Width - w1 - w2

	leftArea := styles.Place(w1, height, lg.Left, lg.Center, opponents.Left)
	centerArea := styles.Place(w2, height, lg.Center, lg.Center, lg.NewStyle().MarginTop(1).Render(centerStack))
	rightArea := styles.Place(w3, height, lg.Right, lg.Center, opponents.Right)

	return lg.JoinHorizontal(lg.Top, leftArea, centerArea, rightArea)
}

func (m *Model) renderCenterTable() string {
	discardView := components.RenderCard(m.Global.Theme, m.Base.TopDiscard, false)
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

	return m.Global.Theme.Muted.Render("Current Suit: ") +
		lg.NewStyle().Bold(true).Foreground(m.Global.Theme.Text).Render(suitStr)
}

func (m *Model) renderPlayerSection() string {
	statusView := gameview.RenderStatus(m.Global.Theme, m.Base.CurrentPlayer, m.Base.MyTurn, m.Base.TurnRemaining)
	handView := gameview.RenderHand(m.Global.Theme, m.Base.Hand, m.Selected, m.pickingSuit)

	sections := []string{statusView, handView}
	if m.lastActionErr != nil {
		errView := m.Global.Theme.ErrorText.Render(m.lastActionErr.Error())
		sections = append(sections, errView)
	}

	return lg.JoinVertical(lg.Center, sections...)
}

func (m *Model) renderSuitPicker() string {
	if !m.pickingSuit {
		return ""
	}

	t := m.Global.Theme
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
