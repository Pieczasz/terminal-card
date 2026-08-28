package hearts

import (
	"slices"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/hearts"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	gameview "github.com/Pieczasz/terminal-card/internal/tui/views/game"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testUser(id uint, name string) *db.User {
	return &db.User{ID: id, Username: name}
}

func startedTable(t *testing.T) (*game.Engine, *Model) {
	t.Helper()
	players := []*game.Player{
		{ID: "1", UserID: 1, Name: "alice"},
		{ID: "2", UserID: 2, Name: "bob"},
		{ID: "3", UserID: 3, Name: "carol"},
		{ID: "4", UserID: 4, Name: "dave"},
	}
	engine := game.NewEngine(&logic.Rules{}, players, deck.StandardDeck())
	require.NoError(t, engine.Start())
	t.Cleanup(engine.Close)

	global := router.GlobalContext{User: testUser(1, "alice"), Width: 80, Height: 40}
	m, ok := New(global, engine).(*Model)
	require.True(t, ok)
	return engine, m
}

func TestSyncState_LoadsHeartsExtra(t *testing.T) {
	t.Parallel()
	_, m := startedTable(t)

	assert.Equal(t, 1, m.handNumber)
	assert.Contains(t, []logic.Stage{logic.StagePassing, logic.StageTrickPlay}, m.stage)
	assert.Len(t, m.seatOrder, 4)
	assert.Equal(t, "alice", m.seatNames["1"])
	assert.False(t, m.heartsBroken)
	assert.Equal(t, logic.DefaultTargetScore, 100)
}

func TestClose_ReleasesEngineSubscription(t *testing.T) {
	t.Parallel()
	engine, m := startedTable(t)
	require.Equal(t, 1, engine.Broadcaster().Len())
	m.Close()
	assert.Zero(t, engine.Broadcaster().Len())
}

// The pass staging used to be keyed by hand position, which survives a re-sort or a
// re-deal underneath it and then passes cards the player never picked. Keyed by card,
// a hand that moves around keeps the same three cards staged.
func TestPassSelection_FollowsTheCardsNotThePositions(t *testing.T) {
	t.Parallel()

	hand := []deck.Card{
		{Rank: deck.Two, Suit: deck.Clubs},
		{Rank: deck.Five, Suit: deck.Hearts},
		{Rank: deck.King, Suit: deck.Spades},
		{Rank: deck.Nine, Suit: deck.Diamonds},
	}
	m := &Model{
		Base:         gameview.BaseState{Hand: slices.Clone(hand), MyTurn: true},
		passSelected: map[deck.Card]struct{}{},
		stage:        logic.StagePassing,
	}

	m.Selected = 1
	m.handleSpace()
	m.Selected = 2
	m.handleSpace()
	require.Len(t, m.passSelected, 2)

	// The same cards, dealt back in a different order.
	m.Base.Hand = []deck.Card{hand[2], hand[3], hand[0], hand[1]}
	assert.Equal(t, map[int]struct{}{0: {}, 3: {}}, m.passIndices(),
		"the markers move with the cards")

	// A card that left the hand stops being staged.
	m.Base.Hand = []deck.Card{hand[0], hand[2]}
	m.prunePassSelection()
	assert.Equal(t, map[deck.Card]struct{}{hand[2]: {}}, m.passSelected)

	// And the staging goes entirely once the pass is over.
	m.stage = logic.StageTrickPlay
	m.prunePassSelection()
	assert.Empty(t, m.passSelected)
}

// A view that skips Close parks a listener goroutine and holds a broadcaster slot for
// every disconnected player until the engine itself is closed.
func TestClose_IsIdempotentAndSurvivesAClosedEngine(t *testing.T) {
	t.Parallel()
	engine, m := startedTable(t)
	require.Equal(t, 1, engine.Broadcaster().Len())

	m.Close()
	assert.Zero(t, engine.Broadcaster().Len())

	assert.NotPanics(t, m.Close, "session teardown may follow a view that already exited")

	engine.Close()
	assert.NotPanics(t, m.Close, "and may follow the engine going away")
}
