package game

import (
	"testing"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/player"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type bindRules struct{}

func (bindRules) Name() string                                      { return "BindTest" }
func (bindRules) MinPlayers() int                                   { return 2 }
func (bindRules) MaxPlayers() int                                   { return 4 }
func (bindRules) InitialDeck() []deck.Card                          { return deck.StandardDeck() }
func (bindRules) InitialDealCount() int                             { return 2 }
func (bindRules) OnGameStart(*State) error                          { return nil }
func (bindRules) PreActionCondition(*State, Action) error           { return nil }
func (bindRules) PostActionCondition(*State, Action) error          { return nil }
func (bindRules) ApplyAction(*State, Action)                        {}
func (bindRules) CheckWinCondition(*State) bool                     { return false }
func (bindRules) GetStandings(s *State) []*player.Player            { return s.Players }

func TestBoundEngine_HandIsClonedAndScoped(t *testing.T) {
	t.Parallel()

	p1 := &player.Player{ID: "1"}
	p2 := &player.Player{ID: "2"}
	engine := NewGameEngine(bindRules{}, []*player.Player{p1, p2}, deck.StandardDeck())
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

type noopAction struct{}

func (noopAction) Name() string { return "noop" }

func TestBoundEngine_SubmitRequiresBoundPlayer(t *testing.T) {
	t.Parallel()

	p1 := &player.Player{ID: "1"}
	p2 := &player.Player{ID: "2"}
	engine := NewGameEngine(bindRules{}, []*player.Player{p1, p2}, deck.StandardDeck())
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
