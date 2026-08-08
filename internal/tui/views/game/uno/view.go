package uno

import (
	"image/color"
	"strings"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/uno"
	"github.com/Pieczasz/terminal-card/internal/tui/components"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"
	gameview "github.com/Pieczasz/terminal-card/internal/tui/views/game"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
)

const keyHints = "<-/h: left | ->/l: right | enter: play/confirm | d: draw | esc: leave/cancel"

var colorLabels = []string{"♥ Red", "♦ Yellow", "♣ Green", "♠ Blue"}

var colorCellWidth = widestLabel(colorLabels) + 4

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
	if m.pickingColor {
		centerStack = m.renderColorPicker()
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
	return lg.JoinVertical(lg.Center,
		discardView,
		m.renderCurrentColorIndicator(),
		m.renderDirectionIndicator(),
	)
}

func (m *Model) renderCurrentColorIndicator() string {
	label, fg := colorLabel(m.global.Theme, m.currentColor)
	if label == "" {
		return ""
	}
	return m.global.Theme.Muted.Render("Color: ") +
		lg.NewStyle().Bold(true).Foreground(fg).Render(label)
}

func (m *Model) renderDirectionIndicator() string {
	dir := "⟳ Clockwise"
	if m.direction < 0 {
		dir = "⟲ Counterclockwise"
	}
	return m.global.Theme.Dim.Render(dir)
}

func colorLabel(t styles.Theme, s deck.Suit) (string, color.Color) {
	switch s {
	case logic.ColorRed:
		return "Red", t.UnoRed
	case logic.ColorYellow:
		return "Yellow", t.UnoYellow
	case logic.ColorGreen:
		return "Green", t.UnoGreen
	case logic.ColorBlue:
		return "Blue", t.UnoBlue
	default:
		return "", t.TextMuted
	}
}

func (m *Model) renderPlayerSection() string {
	statusView := gameview.RenderStatus(m.global.Theme, m.baseState.CurrentPlayer, m.baseState.MyTurn)
	colorRow := m.renderHandColorRow()
	handView := gameview.RenderHand(m.global.Theme, m.baseState.Hand, m.selectedCardIdx, m.pickingColor)

	sections := []string{statusView}
	if colorRow != "" {
		sections = append(sections, colorRow)
	}
	sections = append(sections, handView)
	if m.baseState.MyTurn {
		if clock := gameview.RenderTurnClock(m.global.Theme, m.baseState.TurnRemaining, true); clock != "" {
			sections = append(sections, clock)
		}
	}
	if m.lastActionErr != nil {
		sections = append(sections, m.global.Theme.ErrorText.Render(m.lastActionErr.Error()))
	}
	return lg.JoinVertical(lg.Center, sections...)
}

// renderHandColorRow paints a Uno color glyph above each card so four colors stay
// distinct without changing shared suitStyle (which only has red/dark).
func (m *Model) renderHandColorRow() string {
	hand := m.baseState.Hand
	n := len(hand)
	if n == 0 {
		return ""
	}
	selected := m.selectedCardIdx
	if m.pickingColor {
		selected = -1
	}
	parts := make([]string, 0, n)
	for i, c := range hand {
		w := components.CardSlotWidth(i, n, selected)
		glyph, fg := colorGlyph(m.global.Theme, c)
		cell := lg.NewStyle().Foreground(fg).Width(w).Align(lg.Center).Render(glyph)
		parts = append(parts, cell)
	}
	return strings.Join(parts, "")
}

func colorGlyph(t styles.Theme, c deck.Card) (string, color.Color) {
	if c.Rank == logic.Wild || c.Rank == logic.WildDrawFour {
		return "★", t.TextMuted
	}
	switch c.Suit {
	case logic.ColorRed:
		return "●", t.UnoRed
	case logic.ColorYellow:
		return "●", t.UnoYellow
	case logic.ColorGreen:
		return "●", t.UnoGreen
	case logic.ColorBlue:
		return "●", t.UnoBlue
	default:
		return "·", t.TextMuted
	}
}

func (m *Model) renderColorPicker() string {
	if !m.pickingColor {
		return ""
	}

	t := m.global.Theme
	colors := []color.Color{t.UnoRed, t.UnoYellow, t.UnoGreen, t.UnoBlue}
	rendered := make([]string, 0, len(colorLabels))
	for i, name := range colorLabels {
		style := lg.NewStyle().Padding(0, 1).Border(lg.RoundedBorder()).Foreground(colors[i])
		if i == m.colorCursor {
			style = style.BorderForeground(t.Selection).Bold(true)
		} else {
			style = style.BorderForeground(t.BorderMuted)
		}
		rendered = append(rendered, style.Width(colorCellWidth).Align(lg.Center).Render(name))
	}

	row1 := lg.JoinHorizontal(lg.Center, rendered[0], rendered[1])
	row2 := lg.JoinHorizontal(lg.Center, rendered[2], rendered[3])
	pickerBox := lg.JoinVertical(lg.Center, row1, row2)

	return lg.NewStyle().
		Border(lg.RoundedBorder()).
		BorderForeground(t.Selection).
		Padding(1, 2).
		Render(
			lg.JoinVertical(lg.Center,
				lg.NewStyle().Bold(true).Foreground(t.Selection).Render("Pick a color:"),
				"",
				pickerBox,
			),
		)
}
