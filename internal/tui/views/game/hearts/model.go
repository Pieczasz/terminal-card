package hearts

import (
	"maps"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/hearts"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	gameview "github.com/Pieczasz/terminal-card/internal/tui/views/game"

	tea "charm.land/bubbletea/v2"
)

// TopDiscard from the base state is unused — Hearts has no discard pile.
type Model struct {
	gameview.Session

	// passSelected holds hand indices staged for ActionPassCards (space toggles).
	passSelected map[int]struct{}

	stage            logic.Stage
	heartsBroken     bool
	trickCards       map[string]deck.Card
	handPoints       map[string]int
	cumulativeScores map[string]int
	handNumber       int
	passDirection    logic.PassDirection
	handComplete     bool
	matchComplete    bool
	lastTrickWinner  string
	seatOrder        []string // player IDs clockwise from engine seat 0
	seatNames        map[string]string

	lastActionErr error
}

// New creates a Hearts TUI view bound to the session player.
func New(global router.GlobalContext, engine *game.Engine) tea.Model {
	session, err := gameview.NewSession(global, engine, "hearts")
	m := &Model{
		Session:          session,
		passSelected:     map[int]struct{}{},
		trickCards:       map[string]deck.Card{},
		handPoints:       map[string]int{},
		cumulativeScores: map[string]int{},
		seatNames:        map[string]string{},
		lastActionErr:    err,
	}
	m.syncState()
	return m
}

func (m *Model) syncState() {
	m.SyncBase()
	m.WithHiddenState(func(extra any) {
		if s, ok := extra.(*logic.State); ok {
			m.stage = s.Stage
			m.heartsBroken = s.HeartsBroken
			m.trickCards = maps.Clone(s.TrickCards)
			m.handPoints = maps.Clone(s.HandPoints)
			m.cumulativeScores = maps.Clone(s.CumulativeScores)
			m.handNumber = s.HandNumber
			m.passDirection = s.PassDirection
			m.handComplete = s.HandComplete
			m.matchComplete = s.MatchComplete
			m.lastTrickWinner = s.LastTrickWinner
		}
	})
	m.seatOrder = m.Base.SeatOrder()
	m.seatNames = m.Base.SeatNames()

	for idx := range m.passSelected {
		if idx >= len(m.Base.Hand) {
			delete(m.passSelected, idx)
		}
	}
	if m.stage != logic.StagePassing {
		m.passSelected = map[int]struct{}{}
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.Listen(), gameview.ClockTick())
}
