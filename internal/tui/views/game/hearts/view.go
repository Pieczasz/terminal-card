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
	hints := m.keyHints()
	var fullPlayerArea string
	switch {
	case superCompact:
		fullPlayerArea = mySection
	case compactMode:
		fullPlayerArea = lg.JoinVertical(lg.Center, mySection, m.global.Theme.Dim.Render(hints))
	default:
		fullPlayerArea = lg.NewStyle().MarginBottom(1).Render(
			lg.JoinVertical(lg.Center, mySection, m.global.Theme.Dim.MarginTop(1).Render(hints)),
		)
	}

	topHeight := lg.Height(topAreaContent)
	botHeight := lg.Height(fullPlayerArea)
	midHeight := max(m.global.Height-topHeight-botHeight, 0)

	return tea.NewView(lg.JoinVertical(lg.Left,
		styles.PadCenter(m.global.Width, topAreaContent),
		m.renderMiddleLayer(midHeight, superCompact),
		styles.PadCenter(m.global.Width, fullPlayerArea),
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

	w1 := m.global.Width / 3
	w2 := m.global.Width / 3
	w3 := m.global.Width - w1 - w2

	leftArea := styles.Place(w1, height, lg.Left, lg.Center, leftOpponent)
	centerArea := styles.Place(w2, height, lg.Center, lg.Center, lg.NewStyle().MarginTop(1).Render(centerStack))
	rightArea := styles.Place(w3, height, lg.Right, lg.Center, rightOpponent)

	return lg.JoinHorizontal(lg.Top, leftArea, centerArea, rightArea)
}

func (m *Model) opponentAt(rel int) (game.PlayerSnapshot, bool) {
	// Opponents in BaseState are engine order minus hero. For a 4-seat table that is
	// left=0, top=1, right=2 relative to hero's clockwise neighbours.
	if rel < 0 || rel >= len(m.baseState.Opponents) {
		return game.PlayerSnapshot{}, false
	}
	return m.baseState.Opponents[rel], true
}

func (m *Model) renderTopOpponent(superCompact bool) string {
	o, ok := m.opponentAt(1)
	if !ok {
		return ""
	}
	isTurn := m.baseState.CurrentPlayer == o.Username
	if superCompact {
		return gameview.RenderOpponentMinimal(m.global.Theme, o, isTurn)
	}
	return gameview.RenderOpponent(m.global.Theme, o, isTurn, gameview.OrientationTop, m.baseState.TurnRemaining)
}

func (m *Model) renderSideOpponent(rel int, superCompact bool) string {
	o, ok := m.opponentAt(rel)
	if !ok {
		return ""
	}
	isTurn := m.baseState.CurrentPlayer == o.Username
	orient := gameview.OrientationLeft
	if rel == 2 {
		orient = gameview.OrientationRight
	}
	if superCompact {
		return gameview.RenderOpponentMinimal(m.global.Theme, o, isTurn)
	}
	return gameview.RenderOpponent(m.global.Theme, o, isTurn, orient, m.baseState.TurnRemaining)
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
	if m.bound != nil {
		heroID = m.bound.PlayerID()
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
		return m.global.Theme.Dim.Render(" · ")
	}
	card, ok := m.trickCards[playerID]
	if !ok {
		return m.global.Theme.Dim.Render(" · ")
	}
	return components.RenderCard(m.global.Theme, card, false)
}

func (m *Model) renderHeartsBrokenIndicator() string {
	if m.heartsBroken {
		return lg.NewStyle().Foreground(m.global.Theme.Warning).Render("♥ Hearts: broken")
	}
	return m.global.Theme.Muted.Render("♥ Hearts: not yet broken")
}

func (m *Model) renderPassDirection() string {
	if m.stage != logic.StagePassing {
		return ""
	}
	label := passLabel(m.passDirection)
	return m.global.Theme.Dim.Render("Pass: " + label)
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
	statusView := gameview.RenderStatus(m.global.Theme, m.baseState.CurrentPlayer, m.baseState.MyTurn)
	var handView string
	if m.stage == logic.StagePassing {
		handView = gameview.RenderHandMulti(m.global.Theme, m.baseState.Hand, m.passSelected, m.selectedCardIdx)
	} else {
		handView = gameview.RenderHand(m.global.Theme, m.baseState.Hand, m.selectedCardIdx, false)
	}

	sections := []string{statusView, handView}
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

func (m *Model) renderHandOver() string {
	title := m.global.Theme.Accented.Render(fmt.Sprintf("HAND %d COMPLETE", m.handNumber))
	hint := keyHintsOver
	if m.matchComplete || m.baseState.Phase == game.Finished {
		title = m.global.Theme.Accented.Render("MATCH COMPLETE")
		hint = "esc / enter -> lobby"
		if m.baseState.Winner != "" {
			title = m.global.Theme.Accented.Render("MATCH COMPLETE — " + m.baseState.Winner + " wins")
		}
	}

	lines := make([]string, 0, len(m.seatOrder))
	for _, id := range m.seatOrder {
		name := m.seatNames[id]
		line := m.global.Theme.Muted.Render(fmt.Sprintf("%-12s  hand %3s  total %3s",
			styles.PadTruncate(name, 12),
			strconv.Itoa(m.handPoints[id]),
			strconv.Itoa(m.cumulativeScores[id]),
		))
		lines = append(lines, line)
	}

	content := lg.JoinVertical(lg.Center,
		title, "",
		lg.JoinVertical(lg.Left, lines...),
		"", m.global.Theme.Dim.Render(hint),
	)
	return styles.Place(m.global.Width, m.global.Height, lg.Center, lg.Center, content)
}
