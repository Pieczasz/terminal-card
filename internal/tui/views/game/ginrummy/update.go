package ginrummy

import (
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/ginrummy"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/views"
	gameview "github.com/Pieczasz/terminal-card/internal/tui/views/game"

	tea "charm.land/bubbletea/v2"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if handled, cmd := views.HandleCommonMsg(msg, &m.Global); handled {
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case gameview.EventMsg:
		if m.IdleRemoved(game.Event(msg)) {
			// The engine took this seat for repeated missed turns; quitting ends the
			// bubbletea program, which tears the ssh session down the ordinary way.
			return m, tea.Quit
		}
		m.syncState()
		return m, m.Listen()
	case gameview.ClockTickMsg:
		m.syncState()
		if m.Base.Phase != game.Playing {
			return m, nil
		}
		return m, gameview.ClockTickFor(m.Base.TurnRemaining, m.Base.MyTurn)
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m, m.Leave()
	case "left", "h":
		m.MoveCursor(-1)
		return m, nil
	case "right", "l":
		m.MoveCursor(1)
		return m, nil
	case "0", "1", "2", "3", "4", "5", "6", "7", "8", "9":
		m.SelectDigit(msg.String())
		return m, nil
	case "s":
		return m.submitIfTurn(logic.ActionDrawStock{})
	case "t":
		return m.submitIfTurn(logic.ActionDrawDiscard{})
	case "k":
		return m.handleKnock()
	case "enter":
		return m.handleEnter()
	}
	return m, nil
}

func (m *Model) handleEnter() (tea.Model, tea.Cmd) {
	if m.Base.Phase == game.Finished {
		return m, m.Leave()
	}

	if m.handComplete && !m.matchComplete {
		if m.Base.MyTurn {
			return m.submit(logic.ActionNextHand{})
		}
		return m, nil
	}

	if !m.Base.MyTurn || len(m.Base.Hand) == 0 {
		return m, nil
	}
	card, ok := m.SelectedCard()
	if !ok || m.handPhase != logic.AwaitingDiscard {
		return m, nil
	}
	return m.submit(logic.ActionDiscard{Card: card})
}

func (m *Model) handleKnock() (tea.Model, tea.Cmd) {
	card, ok := m.SelectedCard()
	if !m.Base.MyTurn || !ok || m.handPhase != logic.AwaitingDiscard {
		return m, nil
	}
	return m.submit(logic.ActionKnock{Discard: card})
}

func (m *Model) submitIfTurn(action game.Action) (tea.Model, tea.Cmd) {
	if !m.Base.MyTurn {
		return m, nil
	}
	return m.submit(action)
}

func (m *Model) submit(action game.Action) (tea.Model, tea.Cmd) {
	m.lastActionErr = m.Submit(action)
	return m, nil
}

var _ router.Closer = (*Model)(nil)
