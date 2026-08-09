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
	if handled, cmd := views.HandleCommonMsg(msg, &m.Global); handled {
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case gameview.EventMsg:
		if m.IdleRemoved(game.Event(msg)) {
			// The engine took this seat for repeated missed turns. Quitting ends the
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
	switch key := msg.String(); key {
	case "esc":
		return m.handleEscape()
	case "left", "h":
		return m.moveCursor(-1)
	case "right", "l":
		return m.moveCursor(1)
	case "up", "k":
		return m.moveColorCursor(-2)
	case "down", "j":
		return m.moveColorCursor(2)
	case "0", "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if !m.pickingColor {
			m.SelectDigit(key)
		}
		return m, nil
	case "enter":
		return m.handleEnter()
	case "d":
		return m.handleDraw()
	}
	return m, nil
}

func (m *Model) handleEscape() (tea.Model, tea.Cmd) {
	if m.pickingColor {
		m.pickingColor = false
		return m, nil
	}
	return m, m.Leave()
}

// moveCursor steps the hand, or the colour picker's row when it is open.
func (m *Model) moveCursor(delta int) (tea.Model, tea.Cmd) {
	if !m.pickingColor {
		m.MoveCursor(delta)
		return m, nil
	}
	// The picker is a 2x2 grid: left/right only move within a row.
	if next := m.colorCursor + delta; next/2 == m.colorCursor/2 && next >= 0 && next < len(colorPickerOrder) {
		m.colorCursor = next
	}
	return m, nil
}

func (m *Model) moveColorCursor(delta int) (tea.Model, tea.Cmd) {
	if !m.pickingColor {
		return m, nil
	}
	if next := m.colorCursor + delta; next >= 0 && next < len(colorPickerOrder) {
		m.colorCursor = next
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

	if m.pickingColor {
		return m.submitColorPick(card)
	}

	if card.Rank == logic.Wild || card.Rank == logic.WildDrawFour {
		m.pickingColor = true
		m.colorCursor = 0
		return m, nil
	}

	return m.submit(logic.ActionPlayCard{Card: card})
}

// colorPickerOrder maps the picker grid position to a color; must match view.go.
var colorPickerOrder = []deck.Suit{logic.ColorRed, logic.ColorYellow, logic.ColorGreen, logic.ColorBlue}

func (m *Model) submitColorPick(card deck.Card) (tea.Model, tea.Cmd) {
	if m.colorCursor < 0 || m.colorCursor >= len(colorPickerOrder) {
		return m, nil
	}
	m.pickingColor = false
	return m.submit(logic.ActionPlayCard{
		Card:        card,
		ChosenColor: colorPickerOrder[m.colorCursor],
	})
}

func (m *Model) handleDraw() (tea.Model, tea.Cmd) {
	if !m.Base.MyTurn || m.pickingColor {
		return m, nil
	}
	return m.submit(logic.ActionDrawCard{})
}

func (m *Model) submit(action game.Action) (tea.Model, tea.Cmd) {
	m.lastActionErr = m.Submit(action)
	return m, nil
}

var _ router.Closer = (*Model)(nil)
