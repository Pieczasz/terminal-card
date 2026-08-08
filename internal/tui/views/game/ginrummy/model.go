package ginrummy

import (
	"fmt"
	"log/slog"
	"maps"

	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/ginrummy"
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

	handPhase        logic.Phase
	handNumber       int
	cumulativeScores map[string]int
	handComplete     bool
	matchComplete    bool
	lastHandResult   *logic.HandResult
	stockSize        int
	seatOrder        []string
	seatNames        map[string]string

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

// New creates a Gin Rummy TUI view bound to the session player.
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
				slog.Error("gin rummy view could not subscribe to game events", "error", subErr, "player_id", playerID)
				subErr = fmt.Errorf("live table updates unavailable, leave and rejoin: %w", subErr)
			}
		}
	}
	m := &Model{
		global:           global,
		bound:            bound,
		events:           ch,
		cumulativeScores: map[string]int{},
		seatNames:        map[string]string{},
		lastActionErr:    subErr,
	}
	m.syncState()
	return m
}

func (m *Model) syncState() {
	m.baseState = gameview.SyncBaseState(m.bound)

	if m.bound != nil {
		m.bound.WithExtra(func(extra any) {
			if s, ok := extra.(*logic.State); ok {
				m.handPhase = s.HandPhase
				m.handNumber = s.HandNumber
				m.cumulativeScores = cloneInts(s.CumulativeScores)
				m.handComplete = s.HandComplete
				m.matchComplete = s.MatchComplete
				m.lastHandResult = s.LastHandResult
			}
		})
		if eng := m.bound.Engine(); eng != nil {
			eng.WithState(func(s *game.State) {
				if s.Deck != nil {
					m.stockSize = s.Deck.Size()
				}
				m.seatOrder = make([]string, len(s.Players))
				m.seatNames = make(map[string]string, len(s.Players))
				for i, p := range s.Players {
					m.seatOrder[i] = p.ID
					name := p.ID
					if p.DatabaseUser != nil {
						name = p.DatabaseUser.Username
					}
					m.seatNames[p.ID] = name
				}
			})
		}
	}

	if m.selectedCardIdx >= len(m.baseState.Hand) {
		m.selectedCardIdx = max(len(m.baseState.Hand)-1, 0)
	}
}

func cloneInts(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	maps.Copy(out, in)
	return out
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		listenForEvents(m.events),
		gameview.ClockTick(),
	)
}
