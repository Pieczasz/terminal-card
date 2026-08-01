package crazyeight

import (
	"fmt"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/crazyeight"
	"github.com/Pieczasz/terminal-card/internal/tui/animation"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	gameview "github.com/Pieczasz/terminal-card/internal/tui/views/game"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/harmonica"
)

type gameMsg game.Event

type Model struct {
	global router.GlobalContext
	bound  *game.BoundEngine
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

// New creates a new Crazy Eights TUI view bound to the session player.
func New(global router.GlobalContext, engine *game.Engine) tea.Model {
	playerID := ""
	if global.User != nil {
		playerID = fmt.Sprint(global.User.ID)
	}
	bound := game.Bind(engine, playerID)

	var ch <-chan game.Event
	if bound != nil {
		if b := bound.Broadcaster(); b != nil {
			ch = b.Subscribe()
		}
	}
	m := &Model{
		global:          global,
		bound:           bound,
		events:          ch,
		selectionSpring: animation.DefaultSpring(),
		selectionLift:   2.0,
		selectionVel:    0,
	}
	m.syncState()
	return m
}

func (m *Model) syncState() {
	m.baseState = gameview.SyncBaseState(m.global, m.bound)

	if m.bound != nil {
		m.bound.WithExtra(func(extra any) {
			if s, ok := extra.(*logic.State); ok {
				m.currentSuit = s.CurrentSuit
			}
		})
	}

	if m.selectedCardIdx >= len(m.baseState.Hand) {
		m.selectedCardIdx = max(len(m.baseState.Hand)-1, 0)
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		listenForEvents(m.events),
		animation.Tick(),
	)
}
