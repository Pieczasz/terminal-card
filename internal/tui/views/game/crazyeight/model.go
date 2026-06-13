package crazyeight

import (
	"terminalcard/internal/deck"
	"terminalcard/internal/game"
	logic "terminalcard/internal/game/crazyeight"
	"terminalcard/internal/tui/animation"
	"terminalcard/internal/tui/router"
	gameview "terminalcard/internal/tui/views/game"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/harmonica"
)

type gameMsg game.Event

type Model struct {
	global router.GlobalContext
	engine *game.Engine
	events <-chan game.Event

	baseState       gameview.BaseState
	selectedCardIdx int

	// Crazy Eights specific
	currentSuit     deck.Suit
	pickingSuit     bool
	suitCursor      int
	lastActionErr   error
	selectionSpring harmonica.Spring
	selectionLift   float64
	selectionVel    float64
}

func listenForEvents(ch <-chan game.Event) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return nil
		}
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return gameMsg(msg)
	}
}

// New creates a new Crazy Eights TUI view.
func New(global router.GlobalContext, engine *game.Engine) tea.Model {
	var ch <-chan game.Event
	if engine != nil {
		ch = engine.Broadcaster().Subscribe()
	}
	m := Model{
		global:          global,
		engine:          engine,
		events:          ch,
		selectionSpring: animation.DefaultSpring(),
		selectionLift:   2.0,
		selectionVel:    0,
	}
	m = m.syncState()
	return m
}

func (m Model) syncState() Model {
	m.baseState = gameview.SyncBaseState(m.global, m.engine)

	if m.engine != nil {
		m.engine.WithState(func(state *game.State) {
			// Extract Crazy Eights specific state
			if extra, ok := state.Extra.(*logic.State); ok {
				m.currentSuit = extra.CurrentSuit
			}
		})
	}

	if m.selectedCardIdx >= len(m.baseState.Hand) {
		m.selectedCardIdx = max(len(m.baseState.Hand)-1, 0)
	}

	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		listenForEvents(m.events),
		animation.Tick(),
	)
}
