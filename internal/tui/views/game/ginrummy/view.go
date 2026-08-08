package ginrummy

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/ginrummy"
	"github.com/Pieczasz/terminal-card/internal/tui/components"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"
	gameview "github.com/Pieczasz/terminal-card/internal/tui/views/game"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
)

func (m *Model) View() tea.View {
	if m.handComplete || m.matchComplete || m.handPhase == logic.HandOver {
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
		m.renderMiddleLayer(midHeight),
		styles.PadCenter(m.global.Width, fullPlayerArea),
	))
}

func (m *Model) keyHints() string {
	switch m.handPhase {
	case logic.AwaitingDraw:
		return "s: draw stock | t: take discard | esc: leave"
	case logic.AwaitingDiscard:
		return "<-/h left | ->/l right | enter: discard | k: knock | esc: leave"
	default:
		return "esc: leave"
	}
}

func (m *Model) renderTopOpponent(superCompact bool) string {
	if len(m.baseState.Opponents) == 0 {
		return ""
	}
	o := m.baseState.Opponents[0]
	isTurn := m.baseState.CurrentPlayer == o.Username
	if superCompact {
		return gameview.RenderOpponentMinimal(m.global.Theme, o, isTurn)
	}
	return gameview.RenderOpponent(m.global.Theme, o, isTurn, gameview.OrientationTop, m.baseState.TurnRemaining)
}

func (m *Model) renderMiddleLayer(height int) string {
	discardView := components.RenderCard(m.global.Theme, m.baseState.TopDiscard, false)
	stockLabel := m.global.Theme.Muted.Render(fmt.Sprintf("stock %d", m.stockSize))
	scores := m.renderScoreLine()
	center := lg.JoinVertical(lg.Center, discardView, stockLabel, scores)

	return styles.Place(m.global.Width, height, lg.Center, lg.Center, center)
}

func (m *Model) renderScoreLine() string {
	parts := make([]string, 0, len(m.seatOrder))
	for _, id := range m.seatOrder {
		parts = append(parts, fmt.Sprintf("%s %d", m.seatNames[id], m.cumulativeScores[id]))
	}
	return m.global.Theme.Dim.Render(fmt.Sprintf("hand %d · %s", m.handNumber, joinSep(parts, "  ")))
}

func joinSep(parts []string, sep string) string {
	return strings.Join(parts, sep)
}

func (m *Model) renderPlayerSection() string {
	statusView := gameview.RenderStatus(m.global.Theme, m.baseState.CurrentPlayer, m.baseState.MyTurn)
	handView := gameview.RenderHand(m.global.Theme, m.baseState.Hand, m.selectedCardIdx, false)

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
	hint := "enter: deal next hand | esc: leave"
	if m.matchComplete || m.baseState.Phase == game.Finished {
		title = m.global.Theme.Accented.Render("MATCH COMPLETE")
		hint = "esc / enter -> lobby"
		if m.baseState.Winner != "" {
			title = m.global.Theme.Accented.Render("MATCH COMPLETE — " + m.baseState.Winner + " wins")
		}
	}

	body := m.renderHandResult()
	scores := make([]string, 0, len(m.seatOrder))
	for _, id := range m.seatOrder {
		scores = append(scores, m.global.Theme.Muted.Render(fmt.Sprintf("%-12s  total %3s",
			styles.PadTruncate(m.seatNames[id], 12),
			strconv.Itoa(m.cumulativeScores[id]),
		)))
	}

	content := lg.JoinVertical(lg.Center,
		title, "",
		body, "",
		lg.JoinVertical(lg.Left, scores...),
		"", m.global.Theme.Dim.Render(hint),
	)
	return styles.Place(m.global.Width, m.global.Height, lg.Center, lg.Center, content)
}

func (m *Model) renderHandResult() string {
	r := m.lastHandResult
	if r == nil {
		return ""
	}
	if r.Wall {
		return lg.NewStyle().Foreground(m.global.Theme.Warning).Render("WALL — stock exhausted, no score")
	}

	banner := ""
	switch {
	case r.Gin:
		banner = m.global.Theme.SuccessText.Render("GIN!")
	case r.Undercut:
		banner = lg.NewStyle().Foreground(m.global.Theme.Warning).Render("UNDERCUT")
	default:
		banner = m.global.Theme.Accented.Render("KNOCK")
	}

	winnerName := m.seatNames[r.Winner]
	delta := m.global.Theme.SuccessText.Render(fmt.Sprintf("+%d → %s", r.ScoreDelta, winnerName))

	knockerMelds := m.renderMeldGroups("knocker melds", r.KnockerMelds, false)
	oppDead := m.renderCardRow("opponent deadwood", r.OpponentDeadwood, false)
	laidOff := ""
	if len(r.LaidOffCards) > 0 {
		laidOff = m.renderCardRow("laid off", r.LaidOffCards, true)
	}

	return lg.JoinVertical(lg.Center, banner, delta, "", knockerMelds, oppDead, laidOff)
}

func (m *Model) renderMeldGroups(label string, melds [][]deck.Card, laidOff bool) string {
	if len(melds) == 0 {
		return m.global.Theme.Dim.Render(label + ": —")
	}
	groups := make([]string, 0, len(melds))
	for _, meld := range melds {
		kind := "SET"
		if len(meld) >= 3 && meld[0].Rank != meld[1].Rank {
			kind = "RUN"
		}
		cards := make([]string, 0, len(meld))
		for _, card := range meld {
			cards = append(cards, components.RenderCard(m.global.Theme, card, laidOff))
		}
		box := lg.JoinHorizontal(lg.Top, cards...)
		border := lg.NewStyle().Border(lg.RoundedBorder()).BorderForeground(m.global.Theme.BorderMuted).Padding(0, 1)
		groups = append(groups, border.Render(lg.JoinVertical(lg.Left, m.global.Theme.Dim.Render(kind), box)))
	}
	return lg.JoinVertical(lg.Left,
		m.global.Theme.Muted.Render(label),
		lg.JoinHorizontal(lg.Top, groups...),
	)
}

func (m *Model) renderCardRow(label string, cards []deck.Card, highlight bool) string {
	if len(cards) == 0 {
		return m.global.Theme.Dim.Render(label + ": —")
	}
	parts := make([]string, 0, len(cards))
	for _, card := range cards {
		parts = append(parts, components.RenderCard(m.global.Theme, card, highlight))
	}
	return lg.JoinVertical(lg.Left,
		m.global.Theme.Muted.Render(label),
		lg.JoinHorizontal(lg.Top, parts...),
	)
}
