package hearts

import (
	"errors"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/hearts"
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
	case " ":
		return m.handleSpace()
	case "enter":
		return m.handleEnter()
	}
	return m, nil
}

func (m *Model) handleSpace() (tea.Model, tea.Cmd) {
	if m.stage != logic.StagePassing || !m.Base.MyTurn {
		return m, nil
	}
	if len(m.Base.Hand) == 0 {
		return m, nil
	}
	idx := m.Selected
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
	if m.Base.Phase == game.Finished {
		return m, m.Leave()
	}

	if m.stage == logic.StageHandOver && !m.matchComplete {
		if m.Base.MyTurn {
			return m.submit(logic.ActionNextHand{})
		}
		return m, nil
	}

	if !m.Base.MyTurn || len(m.Base.Hand) == 0 {
		return m, nil
	}

	if m.stage == logic.StagePassing {
		return m.submitPass()
	}

	card, ok := m.SelectedCard()
	if !ok {
		return m, nil
	}
	return m.submit(logic.ActionPlayCard{Card: card})
}

func (m *Model) submitPass() (tea.Model, tea.Cmd) {
	if len(m.passSelected) != 3 {
		m.lastActionErr = errNeedThreeCards
		return m, nil
	}
	cards := make([]deck.Card, 0, 3)
	for idx := range m.passSelected {
		cards = append(cards, m.Base.Hand[idx])
	}
	return m.submit(logic.ActionPassCards{Cards: cards})
}

var errNeedThreeCards = errors.New("select exactly 3 cards (space to toggle)")

func (m *Model) submit(action game.Action) (tea.Model, tea.Cmd) {
	if m.lastActionErr = m.Submit(action); m.lastActionErr == nil {
		m.passSelected = map[int]struct{}{}
	}
	return m, nil
}

var _ router.Closer = (*Model)(nil)
