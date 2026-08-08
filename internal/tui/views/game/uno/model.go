package uno

import (
	"fmt"
	"log/slog"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/uno"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	gameview "github.com/Pieczasz/terminal-card/internal/tui/views/game"

	tea "charm.land/bubbletea/v2"
)

type gameMsg game.Event

type Model struct {
	global router.GlobalContext
	bound  *game.BoundEngine
	events <-chan game.Event

	baseState       gameview.BaseState
	selectedCardIdx int

	currentColor  deck.Suit
	direction     int8
	pickingColor  bool
	colorCursor   int
	lastActionErr error
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

// New creates a Uno TUI view bound to the session player.
func New(global router.GlobalContext, engine *game.Engine) tea.Model {
	playerID := ""
	if global.User != nil {
		playerID = fmt.Sprint(global.User.ID)
	}
	bound := game.Bind(engine, playerID)

	var ch <-chan game.Event
	var subErr error
	if bound != nil {
		if b := bound.Broadcaster(); b != nil {
			ch, subErr = b.Subscribe()
			if subErr != nil {
				slog.Error("uno view could not subscribe to game events", "error", subErr, "player_id", playerID)
				subErr = fmt.Errorf("live table updates unavailable, leave and rejoin: %w", subErr)
			}
		}
	}
	m := &Model{
		global:        global,
		bound:         bound,
		events:        ch,
		direction:     1,
		lastActionErr: subErr,
	}
	m.syncState()
	return m
}

func (m *Model) syncState() {
	m.baseState = gameview.SyncBaseState(m.bound)

	if m.bound != nil {
		m.bound.WithExtra(func(extra any) {
			if s, ok := extra.(*logic.State); ok {
				m.currentColor = s.CurrentColor
				m.direction = s.Direction
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
		gameview.ClockTick(),
	)
}
