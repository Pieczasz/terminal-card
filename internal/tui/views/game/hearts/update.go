package hearts

import (
	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/hearts"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/views"
	gameview "github.com/Pieczasz/terminal-card/internal/tui/views/game"

	tea "charm.land/bubbletea/v2"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if handled, cmd := views.HandleCommonMsg(msg, &m.global); handled {
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case gameMsg:
		if m.idleRemoved(game.Event(msg)) {
			return m, tea.Quit
		}
		m.syncState()
		return m, listenForEvents(m.events)
	case gameview.ClockTickMsg:
		m.syncState()
		if m.baseState.Phase != game.Playing {
			return m, nil
		}
		return m, gameview.ClockTickFor(m.baseState.TurnRemaining, m.baseState.MyTurn)
	}

	return m, nil
}

func (m *Model) idleRemoved(ev game.Event) bool {
	return ev.Type == game.EventPlayerIdle && m.bound != nil && ev.PlayerID == m.bound.PlayerID()
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.handleEscape()
	case "left", "h":
		if m.selectedCardIdx > 0 {
			m.selectedCardIdx--
		}
		return m, nil
	case "right", "l":
		if m.selectedCardIdx < len(m.baseState.Hand)-1 {
			m.selectedCardIdx++
		}
		return m, nil
	case "0", "1", "2", "3", "4", "5", "6", "7", "8", "9":
		return m.handleNumberSelection(msg.String())
	case " ":
		return m.handleSpace()
	case "enter":
		return m.handleEnter()
	}
	return m, nil
}

func (m *Model) unsubscribe() {
	if m.bound != nil && m.events != nil {
		if b := m.bound.Broadcaster(); b != nil {
			b.Unsubscribe(m.events)
		}
		m.events = nil
	}
}

func (m *Model) handleEscape() (tea.Model, tea.Cmd) {
	p := views.SessionPlayer(m.global)

	if m.baseState.Phase == game.Finished {
		m.unsubscribe()
		if p == nil {
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: router.RouteHome} }
		}
		l := m.global.LobbyManager.FindLobbyByPlayer(p)
		return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: router.RouteLobby, Context: l} }
	}

	if p != nil {
		m.global.LobbyManager.LeaveLobby(p)
	}
	m.unsubscribe()
	return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: router.RouteHome} }
}

func (m *Model) handleNumberSelection(key string) (tea.Model, tea.Cmd) {
	if len(m.baseState.Hand) == 0 {
		return m, nil
	}
	idx := int(key[0] - '0')
	if idx < len(m.baseState.Hand) {
		m.selectedCardIdx = idx
	}
	return m, nil
}

func (m *Model) handleSpace() (tea.Model, tea.Cmd) {
	if m.stage != logic.StagePassing || !m.baseState.MyTurn {
		return m, nil
	}
	if len(m.baseState.Hand) == 0 {
		return m, nil
	}
	idx := m.selectedCardIdx
	if _, ok := m.passSelected[idx]; ok {
		delete(m.passSelected, idx)
		return m, nil
	}
	if len(m.passSelected) >= 3 {
		return m, nil
	}
	m.passSelected[idx] = struct{}{}
	return m, nil
}

func (m *Model) handleEnter() (tea.Model, tea.Cmd) {
	if m.baseState.Phase == game.Finished {
		return m.handleEscape()
	}

	if m.stage == logic.StageHandOver && !m.matchComplete {
		if m.baseState.MyTurn {
			return m.submit(logic.ActionNextHand{})
		}
		return m, nil
	}

	if !m.baseState.MyTurn || len(m.baseState.Hand) == 0 {
		return m, nil
	}

	if m.stage == logic.StagePassing {
		return m.submitPass()
	}

	card := m.baseState.Hand[m.selectedCardIdx]
	return m.submit(logic.ActionPlayCard{Card: card})
}

func (m *Model) submitPass() (tea.Model, tea.Cmd) {
	if len(m.passSelected) != 3 {
		m.lastActionErr = errNeedThreeCards
		return m, nil
	}
	cards := make([]deck.Card, 0, 3)
	for idx := range m.passSelected {
		cards = append(cards, m.baseState.Hand[idx])
	}
	return m.submit(logic.ActionPassCards{Cards: cards})
}

var errNeedThreeCards = errString("select exactly 3 cards (space to toggle)")

type errString string

func (e errString) Error() string { return string(e) }

func (m *Model) submit(action game.Action) (tea.Model, tea.Cmd) {
	if err := m.bound.Submit(action); err != nil {
		m.lastActionErr = err
	} else {
		m.lastActionErr = nil
		m.passSelected = map[int]struct{}{}
	}
	return m, nil
}

func (m *Model) Close() {
	m.unsubscribe()
}
