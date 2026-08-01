package poker

import (
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/poker"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/views"

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
		m.lastErr = nil
		m.syncState()
		return m, listenForEvents(m.events)
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
		m.stepRaise(-1)
		return m, nil
	case "]", "l":
		m.stepRaise(+1)
		return m, nil
	case "enter":
		return m.confirm()
	}
	return m, nil
}

// stepRaise nudges the pending raise by one bet increment, staying inside the legal
// range. It is a no-op unless the raise prompt is open.
func (m *Model) stepRaise(direction int) {
	if !m.raising {
		return
	}
	step := m.minRaise
	if step == 0 {
		step = logic.DefaultBigBlind
	}
	if direction < 0 {
		// uint: only subtract when it would not wrap.
		if m.raiseAmount > step {
			m.raiseAmount -= step
		}
	} else {
		m.raiseAmount += step
	}
	m.raiseAmount = m.clampRaise(m.raiseAmount)
}

// confirm either leaves a finished hand or commits the pending raise.
func (m *Model) confirm() (tea.Model, tea.Cmd) {
	if m.baseState.Phase == game.Finished {
		return m.returnToLobby()
	}
	if m.raising && m.baseState.MyTurn {
		amount := m.raiseAmount
		m.raising = false
		return m.submit(logic.ActionRaiseTo{Amount: amount})
	}
	return m, nil
}

func (m *Model) beginRaise() (tea.Model, tea.Cmd) {
	if !m.canRaise() {
		return m, nil
	}
	m.raising = true
	m.raiseAmount = m.clampRaise(m.currentBet + m.minRaise)
	return m, nil
}

func (m *Model) submit(action game.Action) (tea.Model, tea.Cmd) {
	if m.bound == nil || !m.baseState.MyTurn {
		return m, nil
	}
	if err := m.bound.Submit(action); err != nil {
		m.lastErr = err
		return m, nil
	}
	// Re-sync immediately so the turn indicator and actions reflect the applied
	// move without waiting for the broadcast event to round-trip.
	m.lastErr = nil
	m.raising = false
	m.syncState()
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
	if m.raising {
		m.raising = false
		return m, nil
	}
	if m.baseState.Phase == game.Finished {
		return m.returnToLobby()
	}
	p := views.SessionPlayer(m.global)
	if p == nil {
		m.unsubscribe()
		return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: router.RouteHome} }
	}
	m.global.LobbyManager.LeaveLobby(p)
	m.unsubscribe()
	return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: router.RouteHome} }
}

func (m *Model) returnToLobby() (tea.Model, tea.Cmd) {
	p := views.SessionPlayer(m.global)
	var l any
	if p != nil {
		l = m.global.LobbyManager.FindLobbyByPlayer(p)
	}
	m.unsubscribe()
	return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: router.RouteLobby, Context: l} }
}

// Close releases the engine subscription when the router replaces this view or the
// session ends. Without it a mid-game disconnect never runs the esc/enter paths,
// so the listener goroutine stays parked on the event channel.
func (m *Model) Close() {
	m.unsubscribe()
}
