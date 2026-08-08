package poker

import (
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/poker"
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
			// The engine took this seat for repeated missed turns. Quitting ends the
			// bubbletea program, which is what tears the ssh session down and runs the
			// ordinary leave path.
			return m, tea.Quit
		}
		m.lastErr = nil
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

// idleRemoved reports whether ev says this session's own player lost their seat for
// idling. Everyone else's removal is just another state change.
func (m *Model) idleRemoved(ev game.Event) bool {
	return ev.Type == game.EventPlayerIdle && m.bound != nil && ev.PlayerID == m.bound.PlayerID()
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
	case "1", "2", "3", "4":
		m.addChip(msg.String())
		return m, nil
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

// addChip pushes one chip of the keyed denomination into the pending raise, which
// is how a raise is built up: start at the minimum, then stack chips on top.
func (m *Model) addChip(key string) {
	if !m.raising {
		return
	}
	value, ok := chipForKey(key)
	if !ok {
		return
	}
	m.raiseAmount = m.clampRaise(m.raiseAmount + value)
}

// stepRaise nudges the pending raise by the smallest chip, staying inside the
// legal range. It is a no-op unless the raise prompt is open.
func (m *Model) stepRaise(direction int) {
	if !m.raising {
		return
	}
	step := smallestChip()
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

// confirm deals the next hand, leaves a finished match, or commits the pending raise.
func (m *Model) confirm() (tea.Model, tea.Cmd) {
	if m.matchDone {
		return m.returnToLobby()
	}
	if m.canDeal() {
		return m.submit(logic.ActionNextHand{})
	}
	if m.raising && m.baseState.MyTurn {
		amount := m.raiseAmount
		m.raising = false
		return m.submit(logic.ActionRaiseTo{Amount: amount})
	}
	return m, nil
}

// beginRaise opens the raise prompt at the smallest legal raise, so every chip
// the player then adds is on top of an amount that is already valid.
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
	if m.matchDone {
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
