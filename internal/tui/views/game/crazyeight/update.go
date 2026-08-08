package crazyeight

import (
	"math"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/crazyeight"
	"github.com/Pieczasz/terminal-card/internal/tui/animation"
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
		m.syncState()
		return m, listenForEvents(m.events)
	case gameview.ClockTickMsg:
		m.syncState()
		if m.baseState.Phase != game.Playing {
			return m, nil
		}
		return m, gameview.ClockTickFor(m.baseState.TurnRemaining, m.baseState.MyTurn)
	case animation.FrameMsg:
		m.selectionLift, m.selectionVel = m.selectionSpring.Update(m.selectionLift, m.selectionVel, selectionRest)
		if m.springAtRest() {
			// Stop the loop, or every session re-renders at 60 FPS forever for an
			// animation that has finished moving.
			m.selectionLift, m.selectionVel = selectionRest, 0
			m.animating = false
			return m, nil
		}
		return m, animation.Tick()
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
	if m.pickingSuit {
		m.pickingSuit = false
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

// selectionRest is where the lift animation settles, and selectionEpsilon is how
// close counts as arrived.
const (
	selectionRest    = 2.0
	selectionEpsilon = 0.01
)

// springAtRest reports whether the lift animation has finished moving.
func (m *Model) springAtRest() bool {
	return math.Abs(m.selectionVel) < selectionEpsilon &&
		math.Abs(selectionRest-m.selectionLift) < selectionEpsilon
}

// restartSelectionAnimation drops the lift and starts the frame loop, unless it is
// already running - a second Tick would double the frame rate and the chains would
// never merge back.
func (m *Model) restartSelectionAnimation() tea.Cmd {
	m.selectionLift = 0
	m.selectionVel = 0
	if m.animating {
		return nil
	}
	m.animating = true
	return animation.Tick()
}

func (m *Model) handleLeft() (tea.Model, tea.Cmd) {
	if m.pickingSuit {
		if m.suitCursor%2 != 0 {
			m.suitCursor--
		}
		return m, nil
	}
	if m.selectedCardIdx > 0 {
		m.selectedCardIdx--
		return m, m.restartSelectionAnimation()
	}
	return m, nil
}

func (m *Model) handleRight() (tea.Model, tea.Cmd) {
	if m.pickingSuit {
		if m.suitCursor%2 == 0 {
			m.suitCursor++
		}
		return m, nil
	}
	if m.selectedCardIdx < len(m.baseState.Hand)-1 {
		m.selectedCardIdx++
		return m, m.restartSelectionAnimation()
	}
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
	if len(m.baseState.Hand) > 0 && !m.pickingSuit {
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

	if m.pickingSuit {
		return m.submitSuitPick(card)
	}

	if card.Rank == deck.Eight {
		m.pickingSuit = true
		m.suitCursor = 0
		return m, nil
	}

	if err := m.bound.Submit(logic.ActionPlayCard{
		Cards: []deck.Card{card},
	}); err != nil {
		m.lastActionErr = err
	} else {
		m.lastActionErr = nil
	}
	return m, nil
}

// suitPickerOrder maps the picker grid position to a suit, matching view.go's order.
var suitPickerOrder = []deck.Suit{deck.Spades, deck.Hearts, deck.Diamonds, deck.Clubs}

func (m *Model) submitSuitPick(card deck.Card) (tea.Model, tea.Cmd) {
	if m.suitCursor < 0 || m.suitCursor >= len(suitPickerOrder) {
		return m, nil
	}
	chosenSuit := suitPickerOrder[m.suitCursor]
	if err := m.bound.Submit(logic.ActionPlayCard{
		Cards: []deck.Card{card},
		Suit:  chosenSuit,
	}); err != nil {
		m.lastActionErr = err
	} else {
		m.lastActionErr = nil
	}
	m.pickingSuit = false
	return m, nil
}

func (m *Model) handleDraw() (tea.Model, tea.Cmd) {
	if m.baseState.MyTurn && !m.pickingSuit {
		if err := m.bound.Submit(logic.ActionDrawCard{}); err != nil {
			m.lastActionErr = err
		} else {
			m.lastActionErr = nil
		}
	}
	return m, nil
}

// Close releases the engine subscription when the router replaces this view or the
// session ends. See the poker view for why the esc/enter paths are not enough.
func (m *Model) Close() {
	m.unsubscribe()
}
