package crazyeight

import (
	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/crazyeight"
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
			// The engine took this seat for repeated missed turns. Quitting ends the
			// bubbletea program, which is what tears the ssh session down and runs the
			// ordinary leave path.
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

func (m *Model) handleEscape() (tea.Model, tea.Cmd) {
	if m.pickingSuit {
		m.pickingSuit = false
		return m, nil
	}
	return m, m.Leave()
}

// selectionRest is where the lift animation settles, and selectionEpsilon is how
// close counts as arrived.
func (m *Model) handleLeft() (tea.Model, tea.Cmd) {
	if m.pickingSuit {
		if m.suitCursor%2 != 0 {
			m.suitCursor--
		}
		return m, nil
	}
	m.MoveCursor(-1)
	return m, nil
}

func (m *Model) handleRight() (tea.Model, tea.Cmd) {
	if m.pickingSuit {
		if m.suitCursor%2 == 0 {
			m.suitCursor++
		}
		return m, nil
	}
	m.MoveCursor(1)
	return m, nil
}

func (m *Model) handleUp() (tea.Model, tea.Cmd) {
	if m.pickingSuit {
		if m.suitCursor >= 2 {
			m.suitCursor -= 2
		}
		return m, nil
	}
	return m, nil
}

func (m *Model) handleDown() (tea.Model, tea.Cmd) {
	if m.pickingSuit {
		if m.suitCursor < 2 {
			m.suitCursor += 2
		}
		return m, nil
	}
	return m, nil
}

func (m *Model) handleNumberSelection(key string) (tea.Model, tea.Cmd) {
	if !m.pickingSuit {
		m.SelectDigit(key)
	}
	return m, nil
}

func (m *Model) handleEnter() (tea.Model, tea.Cmd) {
	if m.Base.Phase == game.Finished {
		return m, m.Leave()
	}

	card, ok := m.SelectedCard()
	if !m.Base.MyTurn || !ok {
		return m, nil
	}

	if m.pickingSuit {
		return m.submitSuitPick(card)
	}

	if card.Rank == deck.Eight {
		m.pickingSuit = true
		m.suitCursor = 0
		return m, nil
	}

	m.lastActionErr = m.Submit(logic.ActionPlayCard{Cards: []deck.Card{card}})
	return m, nil
}

// suitPickerOrder maps the picker grid position to a suit, matching view.go's order.
var suitPickerOrder = []deck.Suit{deck.Spades, deck.Hearts, deck.Diamonds, deck.Clubs}

func (m *Model) submitSuitPick(card deck.Card) (tea.Model, tea.Cmd) {
	if m.suitCursor < 0 || m.suitCursor >= len(suitPickerOrder) {
		return m, nil
	}
	m.pickingSuit = false
	m.lastActionErr = m.Submit(logic.ActionPlayCard{
		Cards: []deck.Card{card},
		Suit:  suitPickerOrder[m.suitCursor],
	})
	return m, nil
}

func (m *Model) handleDraw() (tea.Model, tea.Cmd) {
	if m.Base.MyTurn && !m.pickingSuit {
		m.lastActionErr = m.Submit(logic.ActionDrawCard{})
	}
	return m, nil
}

// Close comes from the embedded Session: the router replaces this view on navigation
// and the ssh layer closes it on disconnect, so the esc/enter paths are not enough.
var _ router.Closer = (*Model)(nil)
