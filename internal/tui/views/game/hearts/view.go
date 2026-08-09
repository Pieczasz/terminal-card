package hearts

import (
	"fmt"
	"strconv"

	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/hearts"
	"github.com/Pieczasz/terminal-card/internal/tui/components"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"
	gameview "github.com/Pieczasz/terminal-card/internal/tui/views/game"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
)

const (
	keyHintsPlay = "<-/h: left | ->/l: right | enter: play | esc: leave"
	keyHintsPass = "<-/h: left | ->/l: right | space: toggle | enter: pass 3 | esc: leave"
	keyHintsOver = "enter: next hand | esc: leave match"
)

func (m *Model) View() tea.View {
	if m.handComplete || m.matchComplete || m.stage == logic.StageHandOver {
		return tea.NewView(m.renderHandOver())
	}
	if m.Base.Phase != game.Playing {
		return tea.NewView(gameview.RenderWaitingScreen(m.Global, m.Base.Phase, m.Base.Winner))
	}

	compactMode := m.Global.Height < 30
	superCompact := m.Global.Height < 24

	topSection := m.renderTopOpponent(superCompact)
	var topAreaContent string
	if superCompact {
		topAreaContent = topSection
	} else {
		topAreaContent = lg.NewStyle().MarginTop(1).Render(topSection)
	}

	mySection := m.renderPlayerSection()
	hints := m.keyHints()
	var fullPlayerArea string
	switch {
	case superCompact:
		fullPlayerArea = mySection
	case compactMode:
		fullPlayerArea = lg.JoinVertical(lg.Center, mySection, m.Global.Theme.Dim.Render(hints))
	default:
		fullPlayerArea = lg.NewStyle().MarginBottom(1).Render(
			lg.JoinVertical(lg.Center, mySection, m.Global.Theme.Dim.MarginTop(1).Render(hints)),
		)
	}

	topHeight := lg.Height(topAreaContent)
	botHeight := lg.Height(fullPlayerArea)
	midHeight := max(m.Global.Height-topHeight-botHeight, 0)

	return tea.NewView(lg.JoinVertical(lg.Left,
		styles.PadCenter(m.Global.Width, topAreaContent),
		m.renderMiddleLayer(midHeight, superCompact),
		styles.PadCenter(m.Global.Width, fullPlayerArea),
	))
}

func (m *Model) keyHints() string {
	if m.stage == logic.StagePassing {
		return keyHintsPass
	}
	return keyHintsPlay
}

func (m *Model) renderMiddleLayer(height int, superCompact bool) string {
	leftOpponent := m.renderSideOpponent(0, superCompact)
	rightOpponent := m.renderSideOpponent(2, superCompact)
	centerStack := lg.JoinVertical(lg.Center,
		m.renderTrickArea(),
		m.renderHeartsBrokenIndicator(),
		m.renderPassDirection(),
	)

	w1 := m.Global.Width / 3
	w2 := m.Global.Width / 3
	w3 := m.Global.Width - w1 - w2

	leftArea := styles.Place(w1, height, lg.Left, lg.Center, leftOpponent)
	centerArea := styles.Place(w2, height, lg.Center, lg.Center, lg.NewStyle().MarginTop(1).Render(centerStack))
	rightArea := styles.Place(w3, height, lg.Right, lg.Center, rightOpponent)

	return lg.JoinHorizontal(lg.Top, leftArea, centerArea, rightArea)
}

func (m *Model) opponentAt(rel int) (game.PlayerSnapshot, bool) {
	// Find hero's seat index in full engine order, then rotate clockwise from there.
	// Hearts is always 4 players, so rel is 0/1/2 for left/top/right neighbours.
	heroID := ""
	if m.Bound != nil {
		heroID = m.Bound.PlayerID()
	}
	heroIdx := -1
	for i, seat := range m.Base.Seats {
		if seat.ID == heroID {
			heroIdx = i
			break
		}
	}
	if heroIdx < 0 || rel < 0 || rel >= 3 {
		return game.PlayerSnapshot{}, false
	}
	oppIdx := (heroIdx + rel + 1) % len(m.Base.Seats)
	return m.Base.Seats[oppIdx], true
}

func (m *Model) renderTopOpponent(superCompact bool) string {
	o, ok := m.opponentAt(1)
	if !ok {
		return ""
	}
	isTurn := m.Base.CurrentPlayerID == o.ID
	if superCompact {
		return gameview.RenderOpponentMinimal(m.Global.Theme, o, isTurn)
	}
	return gameview.RenderOpponent(m.Global.Theme, o, isTurn, gameview.OrientationTop, m.Base.TurnRemaining)
}

func (m *Model) renderSideOpponent(rel int, superCompact bool) string {
	o, ok := m.opponentAt(rel)
	if !ok {
		return ""
	}
	isTurn := m.Base.CurrentPlayerID == o.ID
	orient := gameview.OrientationLeft
	if rel == 2 {
		orient = gameview.OrientationRight
	}
	if superCompact {
		return gameview.RenderOpponentMinimal(m.Global.Theme, o, isTurn)
	}
	return gameview.RenderOpponent(m.Global.Theme, o, isTurn, orient, m.Base.TurnRemaining)
}

func (m *Model) opponentID(rel int) string {
	o, ok := m.opponentAt(rel)
	if !ok {
		return ""
	}
	return o.ID
}

func (m *Model) renderTrickArea() string {
	heroID := ""
	if m.Bound != nil {
		heroID = m.Bound.PlayerID()
	}
	hero := m.renderTrickSlot(heroID)
	left := m.renderTrickSlot(m.opponentID(0))
	top := m.renderTrickSlot(m.opponentID(1))
	right := m.renderTrickSlot(m.opponentID(2))
	return lg.JoinVertical(lg.Center,
		top,
		lg.JoinHorizontal(lg.Center, left, "  ", right),
		hero,
	)
}

func (m *Model) renderTrickSlot(playerID string) string {
	if playerID == "" {
		return m.Global.Theme.Dim.Render(" · ")
	}
	card, ok := m.trickCards[playerID]
	if !ok {
		return m.Global.Theme.Dim.Render(" · ")
	}
	return components.RenderCard(m.Global.Theme, card, false)
}

func (m *Model) renderHeartsBrokenIndicator() string {
	if m.heartsBroken {
		return lg.NewStyle().Foreground(m.Global.Theme.Warning).Render("♥ Hearts: broken")
	}
	return m.Global.Theme.Muted.Render("♥ Hearts: not yet broken")
}

func (m *Model) renderPassDirection() string {
	if m.stage != logic.StagePassing {
		return ""
	}
	label := passLabel(m.passDirection)
	return m.Global.Theme.Dim.Render("Pass: " + label)
}

func passLabel(d logic.PassDirection) string {
	switch d {
	case logic.PassLeft:
		return "left"
	case logic.PassRight:
		return "right"
	case logic.PassAcross:
		return "across"
	default:
		return "none"
	}
}

func (m *Model) renderPlayerSection() string {
	statusView := gameview.RenderStatus(m.Global.Theme, m.Base.CurrentPlayer, m.Base.MyTurn, m.Base.TurnRemaining)
	var handView string
	if m.stage == logic.StagePassing {
		handView = gameview.RenderHandMulti(m.Global.Theme, m.Base.Hand, m.passSelected, m.Selected)
	} else {
		handView = gameview.RenderHand(m.Global.Theme, m.Base.Hand, m.Selected, false)
	}

	sections := []string{statusView, handView}
	if m.lastActionErr != nil {
		sections = append(sections, m.Global.Theme.ErrorText.Render(m.lastActionErr.Error()))
	}
	return lg.JoinVertical(lg.Center, sections...)
}

func (m *Model) renderHandOver() string {
	title := m.Global.Theme.Accented.Render(fmt.Sprintf("HAND %d COMPLETE", m.handNumber))
	hint := keyHintsOver
	if m.matchComplete || m.Base.Phase == game.Finished {
		title = m.Global.Theme.Accented.Render("MATCH COMPLETE")
		hint = "esc / enter -> lobby"
		if m.Base.Winner != "" {
			title = m.Global.Theme.Accented.Render("MATCH COMPLETE — " + m.Base.Winner + " wins")
		}
	}

	lines := make([]string, 0, len(m.seatOrder))
	for _, id := range m.seatOrder {
		name := m.seatNames[id]
		line := m.Global.Theme.Muted.Render(fmt.Sprintf("%-12s  hand %3s  total %3s",
			styles.PadTruncate(name, 12),
			strconv.Itoa(m.handPoints[id]),
			strconv.Itoa(m.cumulativeScores[id]),
		))
		lines = append(lines, line)
	}

	content := lg.JoinVertical(lg.Center,
		title, "",
		lg.JoinVertical(lg.Left, lines...),
		"", m.Global.Theme.Dim.Render(hint),
	)
	return styles.Place(m.Global.Width, m.Global.Height, lg.Center, lg.Center, content)
}
