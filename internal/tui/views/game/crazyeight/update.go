package crazyeight

import (
	"fmt"
	"terminalcard/internal/deck"
	"terminalcard/internal/game"
	logic "terminalcard/internal/game/crazyeight"
	"terminalcard/internal/player"
	"terminalcard/internal/tui/router"
	"terminalcard/internal/tui/views"
	"terminalcard/internal/tui/animation"

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
	case animation.FrameMsg:
		m.selectionLift, m.selectionVel = m.selectionSpring.Update(m.selectionLift, m.selectionVel, 2.0)
		return m, animation.Tick()
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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

func (m Model) handleEscape() (tea.Model, tea.Cmd) {
	if m.pickingSuit {
		m.pickingSuit = false
		return m, nil
	}

	p := &player.Player{ID: fmt.Sprint(m.global.User.ID), DatabaseUser: m.global.User}

	if m.baseState.Phase == game.Finished {
		l := m.global.LobbyManager.FindLobbyByPlayer(p)
		return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "lobby", Context: l} }
	}

	m.global.LobbyManager.LeaveLobby(p)
	return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "home"} }
}

func (m Model) handleLeft() (tea.Model, tea.Cmd) {
	if m.pickingSuit {
		if m.suitCursor%2 != 0 {
			m.suitCursor--
		}
		return m, nil
	}
	if m.selectedCardIdx > 0 {
		m.selectedCardIdx--
		m.selectionLift = 0
		m.selectionVel = 0
	}
	return m, nil
}

func (m Model) handleRight() (tea.Model, tea.Cmd) {
	if m.pickingSuit {
		if m.suitCursor%2 == 0 {
			m.suitCursor++
		}
		return m, nil
	}
	if m.selectedCardIdx < len(m.baseState.Hand)-1 {
		m.selectedCardIdx++
		m.selectionLift = 0
		m.selectionVel = 0
	}
	return m, nil
}

func (m Model) handleUp() (tea.Model, tea.Cmd) {
	if m.pickingSuit {
		if m.suitCursor >= 2 {
			m.suitCursor -= 2
		}
		return m, nil
	}
	return m, nil
}

func (m Model) handleDown() (tea.Model, tea.Cmd) {
	if m.pickingSuit {
		if m.suitCursor < 2 {
			m.suitCursor += 2
		}
		return m, nil
	}
	return m, nil
}

func (m Model) handleNumberSelection(key string) (tea.Model, tea.Cmd) {
	if len(m.baseState.Hand) > 0 && !m.pickingSuit {
		idx := int(key[0] - '0')
		if idx < len(m.baseState.Hand) {
			m.selectedCardIdx = idx
		}
	}
	return m, nil
}

func (m Model) handleEnter() (tea.Model, tea.Cmd) {
	if m.baseState.Phase == game.Finished {
		p := &player.Player{ID: fmt.Sprint(m.global.User.ID), DatabaseUser: m.global.User}
		l := m.global.LobbyManager.FindLobbyByPlayer(p)
		return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "lobby", Context: l} }
	}

	if !m.baseState.MyTurn || len(m.baseState.Hand) == 0 {
		return m, nil
	}

	card := m.baseState.Hand[m.selectedCardIdx]

	if m.pickingSuit {
		return m.submitSuitPick(card)
	}

	if card.Rank == deck.Eight {
		m.pickingSuit = true
		m.suitCursor = 0
		return m, nil
	}

	_ = m.engine.SubmitAction(fmt.Sprint(m.global.User.ID), logic.ActionPlayCard{
		Cards: []deck.Card{card},
	})
	return m, nil
}

func (m Model) submitSuitPick(card deck.Card) (tea.Model, tea.Cmd) {
	chosenSuit := deck.Suit(m.suitCursor)
	_ = m.engine.SubmitAction(fmt.Sprint(m.global.User.ID), logic.ActionPlayCard{
		Cards: []deck.Card{card},
		Suit:  chosenSuit,
	})
	m.pickingSuit = false
	return m, nil
}

func (m Model) handleDraw() (tea.Model, tea.Cmd) {
	if m.baseState.MyTurn && !m.pickingSuit {
		_ = m.engine.SubmitAction(fmt.Sprint(m.global.User.ID), logic.ActionDrawCard{})
	}
	return m, nil
}
