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
	if m.pickingColor {
		centerStack = m.renderColorPicker()
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
	return lg.JoinVertical(lg.Center,
		discardView,
		m.renderCurrentColorIndicator(),
		m.renderDirectionIndicator(),
	)
}

func (m *Model) renderCurrentColorIndicator() string {
	label, fg := colorLabel(m.Global.Theme, m.currentColor)
	if label == "" {
		return ""
	}
	return m.Global.Theme.Muted.Render("Color: ") +
		lg.NewStyle().Bold(true).Foreground(fg).Render(label)
}

func (m *Model) renderDirectionIndicator() string {
	dir := "⟳ Clockwise"
	if m.direction < 0 {
		dir = "⟲ Counterclockwise"
	}
	return m.Global.Theme.Dim.Render(dir)
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
	statusView := gameview.RenderStatus(m.Global.Theme, m.Base.CurrentPlayer, m.Base.MyTurn, m.Base.TurnRemaining)
	colorRow := m.renderHandColorRow()
	handView := gameview.RenderHand(m.Global.Theme, m.Base.Hand, m.Selected, m.pickingColor)

	sections := []string{statusView}
	if colorRow != "" {
		sections = append(sections, colorRow)
	}
	sections = append(sections, handView)
	if m.lastActionErr != nil {
		sections = append(sections, m.Global.Theme.ErrorText.Render(m.lastActionErr.Error()))
	}
	return lg.JoinVertical(lg.Center, sections...)
}

// renderHandColorRow paints a Uno color glyph above each card so four colors stay
// distinct without changing shared suitStyle (which only has red/dark).
func (m *Model) renderHandColorRow() string {
	hand := m.Base.Hand
	n := len(hand)
	if n == 0 {
		return ""
	}
	selected := m.Selected
	if m.pickingColor {
		selected = -1
	}
	parts := make([]string, 0, n)
	for i, c := range hand {
		w := components.CardSlotWidth(i, n, selected)
		glyph, fg := colorGlyph(m.Global.Theme, c)
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

	t := m.Global.Theme
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
