package uno

import (
	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/uno"
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
		return m.handleLeft()
	case "right", "l":
		return m.handleRight()
	case "up", "k":
		return m.handleUp()
	case "down", "j":
		return m.handleDown()
	case "0", "1", "2", "3", "4", "5", "6", "7", "8", "9":
		return m.handleNumberSelection(msg.String())
	case "enter":
		return m.handleEnter()
	case "d":
		return m.handleDraw()
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
	if m.pickingColor {
		m.pickingColor = false
		return m, nil
	}

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

func (m *Model) handleLeft() (tea.Model, tea.Cmd) {
	if m.pickingColor {
		if m.colorCursor%2 != 0 {
			m.colorCursor--
		}
		return m, nil
	}
	if m.selectedCardIdx > 0 {
		m.selectedCardIdx--
	}
	return m, nil
}

func (m *Model) handleRight() (tea.Model, tea.Cmd) {
	if m.pickingColor {
		if m.colorCursor%2 == 0 {
			m.colorCursor++
		}
		return m, nil
	}
	if m.selectedCardIdx < len(m.baseState.Hand)-1 {
		m.selectedCardIdx++
	}
	return m, nil
}

func (m *Model) handleUp() (tea.Model, tea.Cmd) {
	if m.pickingColor && m.colorCursor >= 2 {
		m.colorCursor -= 2
	}
	return m, nil
}

func (m *Model) handleDown() (tea.Model, tea.Cmd) {
	if m.pickingColor && m.colorCursor < 2 {
		m.colorCursor += 2
	}
	return m, nil
}

func (m *Model) handleNumberSelection(key string) (tea.Model, tea.Cmd) {
	if len(m.baseState.Hand) > 0 && !m.pickingColor {
		idx := int(key[0] - '0')
		if idx < len(m.baseState.Hand) {
			m.selectedCardIdx = idx
		}
	}
	return m, nil
}

func (m *Model) handleEnter() (tea.Model, tea.Cmd) {
	if m.baseState.Phase == game.Finished {
		p := views.SessionPlayer(m.global)
		m.unsubscribe()
		if p == nil {
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: router.RouteHome} }
		}
		l := m.global.LobbyManager.FindLobbyByPlayer(p)
		return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: router.RouteLobby, Context: l} }
	}

	if !m.baseState.MyTurn || len(m.baseState.Hand) == 0 {
		return m, nil
	}

	card := m.baseState.Hand[m.selectedCardIdx]

	if m.pickingColor {
		return m.submitColorPick(card)
	}

	if card.Rank == logic.Wild || card.Rank == logic.WildDrawFour {
		m.pickingColor = true
		m.colorCursor = 0
		return m, nil
	}

	if err := m.bound.Submit(logic.ActionPlayCard{Card: card}); err != nil {
		m.lastActionErr = err
	} else {
		m.lastActionErr = nil
	}
	return m, nil
}

// colorPickerOrder maps the picker grid position to a color; must match view.go.
var colorPickerOrder = []deck.Suit{logic.ColorRed, logic.ColorYellow, logic.ColorGreen, logic.ColorBlue}

func (m *Model) submitColorPick(card deck.Card) (tea.Model, tea.Cmd) {
	if m.colorCursor < 0 || m.colorCursor >= len(colorPickerOrder) {
		return m, nil
	}
	if err := m.bound.Submit(logic.ActionPlayCard{
		Card:        card,
		ChosenColor: colorPickerOrder[m.colorCursor],
	}); err != nil {
		m.lastActionErr = err
	} else {
		m.lastActionErr = nil
	}
	m.pickingColor = false
	return m, nil
}

func (m *Model) handleDraw() (tea.Model, tea.Cmd) {
	if m.baseState.MyTurn && !m.pickingColor {
		if err := m.bound.Submit(logic.ActionDrawCard{}); err != nil {
			m.lastActionErr = err
		} else {
			m.lastActionErr = nil
		}
	}
	return m, nil
}

func (m *Model) Close() {
	m.unsubscribe()
}
