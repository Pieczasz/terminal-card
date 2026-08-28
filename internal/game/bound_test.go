package game

import (
	"slices"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/deck"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type bindRules struct{}

func (bindRules) MinPlayers() int                     { return 2 }
func (bindRules) MaxPlayers() int                     { return 4 }
func (bindRules) InitialDeck() []deck.Card            { return deck.StandardDeck() }
func (bindRules) InitialDealCount() int               { return 2 }
func (bindRules) OnGameStart(*State) error            { return nil }
func (bindRules) ValidateAction(*State, Action) error { return nil }
func (bindRules) AfterAction(*State, Action) error    { return nil }
func (bindRules) ApplyAction(*State, Action) error    { return nil }
func (bindRules) CheckWinCondition(*State) bool       { return false }
func (bindRules) Standings(s *State) []*Player        { return s.Players }

func TestBoundEngine_HandIsClonedAndScoped(t *testing.T) {
	t.Parallel()

	p1 := &Player{ID: "1"}
	p2 := &Player{ID: "2"}
	engine := NewEngine(bindRules{}, []*Player{p1, p2}, deck.StandardDeck())
	require.NoError(t, engine.Start())

	bound := Bind(engine, "1")
	hand := bound.Hand()
	require.NotEmpty(t, hand)

	orig := hand[0]
	hand[0] = deck.Card{}
	again := bound.Hand()
	assert.Equal(t, orig, again[0], "mutating Hand copy must not alter engine state")

	other := Bind(engine, "2").Hand()
	assert.Len(t, other, len(hand))
}

// Hand is the redaction boundary: it is the only place a player's own cards are readable,
// so it has to match the bound seat and no other.
func TestBoundEngine_HandBelongsToTheBoundPlayerOnly(t *testing.T) {
	t.Parallel()

	p1 := &Player{ID: "1"}
	p2 := &Player{ID: "2"}
	engine := NewEngine(bindRules{}, []*Player{p1, p2}, deck.StandardDeck())
	t.Cleanup(engine.Close)
	require.NoError(t, engine.Start())

	var dealt1, dealt2 []deck.Card
	engine.WithState(func(state *State) {
		dealt1 = slices.Clone(state.Players[0].Cards)
		dealt2 = slices.Clone(state.Players[1].Cards)
	})
	require.NotEqual(t, dealt1, dealt2, "a shuffled deck must not deal two identical hands")

	assert.Equal(t, dealt1, Bind(engine, "1").Hand(), "each seat sees exactly its own cards")
	assert.Equal(t, dealt2, Bind(engine, "2").Hand())
	assert.Nil(t, Bind(engine, "not-seated").Hand(), "an ID with no seat holds no cards")
}

type noopAction struct{}

func (noopAction) Name() string { return "noop" }

func TestBoundEngine_SubmitRequiresBoundPlayer(t *testing.T) {
	t.Parallel()

	p1 := &Player{ID: "1"}
	p2 := &Player{ID: "2"}
	engine := NewEngine(bindRules{}, []*Player{p1, p2}, deck.StandardDeck())
	require.NoError(t, engine.Start())

	current := engine.CurrentPlayerID()
	other := "1"
	if current == "1" {
		other = "2"
	}
	boundOther := Bind(engine, other)
	err := boundOther.Submit(noopAction{})
	assert.ErrorContains(t, err, "wait for your turn")
}

// A view gets a channel, not the broadcaster: it must be able to join and leave the
// feed without being able to end it for the rest of the table.
func TestBoundEngine_SubscribeAndUnsubscribe(t *testing.T) {
	t.Parallel()

	engine := NewEngine(bindRules{}, []*Player{{ID: "1"}, {ID: "2"}}, deck.StandardDeck())
	t.Cleanup(engine.Close)
	bound := Bind(engine, "1")

	events, err := bound.Subscribe()
	require.NoError(t, err)
	require.Equal(t, 1, engine.Broadcaster().Len())

	require.NoError(t, engine.Start())
	assert.Equal(t, EventGameStarted, (<-events).Type)

	bound.Unsubscribe(events)
	assert.Zero(t, engine.Broadcaster().Len(), "unsubscribing returns the slot")

	var unbound *BoundEngine
	_, err = unbound.Subscribe()
	require.Error(t, err, "a view with no engine is told so rather than handed a nil channel")
	assert.NotPanics(t, func() { unbound.Unsubscribe(nil) })
}
