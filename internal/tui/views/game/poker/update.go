package poker

import (
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/poker"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/views"
	gameview "github.com/Pieczasz/terminal-card/internal/tui/views/game"

	tea "charm.land/bubbletea/v2"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if handled, cmd := views.HandleCommonMsg(msg, &m.global); handled {
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case gameMsg:
		return m.syncState(), listenForEvents(m.events)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.handleEscape()
	case "f":
		if m.canFold() {
			return m.submit(logic.ActionFold{})
		}
		return m, nil
	case "c":
		if m.canCheck() {
			return m.submit(logic.ActionCheck{})
		}
		if m.canCall() {
			return m.submit(logic.ActionCall{})
		}
		return m, nil
	case "a":
		if m.canAllIn() {
			m.raising = false
			return m.submit(logic.ActionAllIn{})
		}
		return m, nil
	case "r":
		return m.beginRaise()
	case "[", "h":
		if m.raising {
			step := m.minRaise
			if step == 0 {
				step = logic.DefaultBigBlind
			}
			if m.raiseAmount > step {
				m.raiseAmount -= step
			}
			minTo := m.currentBet + m.minRaise
			if m.raiseAmount < minTo {
				m.raiseAmount = minTo
			}
		}
		return m, nil
	case "]", "l":
		if m.raising {
			step := m.minRaise
			if step == 0 {
				step = logic.DefaultBigBlind
			}
			m.raiseAmount += step
			maxTo := m.streetBetMax()
			if m.raiseAmount > maxTo {
				m.raiseAmount = maxTo
			}
		}
		return m, nil
	case "enter":
		if m.baseState.Phase == game.Finished {
			return m.returnToLobby()
		}
		if m.raising && m.baseState.MyTurn {
			amt := m.raiseAmount
			m.raising = false
			return m.submit(logic.ActionRaiseTo{Amount: amt})
		}
		return m, nil
	}
	return m, nil
}

func (m Model) beginRaise() (tea.Model, tea.Cmd) {
	if !m.canRaise() {
		return m, nil
	}
	m.raising = true
	m.raiseAmount = m.currentBet + m.minRaise
	maxTo := m.streetBetMax()
	if m.raiseAmount > maxTo {
		m.raiseAmount = maxTo
	}
	return m, nil
}

func (m Model) submit(action game.Action) (tea.Model, tea.Cmd) {
	if m.bound == nil || !m.baseState.MyTurn {
		return m, nil
	}
	if err := m.bound.Submit(action); err != nil {
		m.lastErr = err
	} else {
		m.lastErr = nil
		m.raising = false
	}
	return m, nil
}

func (m Model) unsubscribe() Model {
	if m.bound != nil && m.events != nil {
		if b := m.bound.Broadcaster(); b != nil {
			b.Unsubscribe(m.events)
		}
		m.events = nil
	}
	return m
}

func (m Model) handleEscape() (tea.Model, tea.Cmd) {
	if m.raising {
		m.raising = false
		return m, nil
	}
	if m.baseState.Phase == game.Finished {
		return m.returnToLobby()
	}
	p := gameview.SessionPlayer(m.global)
	if p == nil {
		m = m.unsubscribe()
		return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "home"} }
	}
	m.global.LobbyManager.LeaveLobby(p)
	m = m.unsubscribe()
	return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "home"} }
}

func (m Model) returnToLobby() (tea.Model, tea.Cmd) {
	p := gameview.SessionPlayer(m.global)
	var l any
	if p != nil {
		l = m.global.LobbyManager.FindLobbyByPlayer(p)
	}
	m = m.unsubscribe()
	return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "lobby", Context: l} }
}
