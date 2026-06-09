package crazyeight

import (
	"testing"

	"terminalcard/internal/deck"
	"terminalcard/internal/game"
	"terminalcard/internal/player"

	"github.com/stretchr/testify/assert"
)

func createTestState() *game.State {
	rules := &CrazyEightsRules{}
	players := []*player.Player{{ID: "p1", Cards: []deck.Card{
		{Rank: deck.Two, Suit: deck.Spades},
		{Rank: deck.King, Suit: deck.Hearts},
		{Rank: deck.Eight, Suit: deck.Diamonds},
	}}}
	state := game.NewState(rules, players, deck.StandardDeck())
	state.Extra = &State{CurrentSuit: deck.Spades}
	state.Discard = deck.New([]deck.Card{{Rank: deck.Nine, Suit: deck.Spades}})
	state.CurrentTurn = 0
	return state
}

func TestCrazyEightsRules_PreActionCondition_PlayCard(t *testing.T) {
	t.Parallel()

	t.Run("valid matching suit", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		rules := &CrazyEightsRules{}
		action := ActionPlayCard{Cards: []deck.Card{{Rank: deck.Two, Suit: deck.Spades}}}

		err := rules.PreActionCondition(state, action)
		assert.NoError(t, err)
	})

	t.Run("valid matching rank", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		state.Discard = deck.New([]deck.Card{{Rank: deck.King, Suit: deck.Clubs}})
		rules := &CrazyEightsRules{}
		action := ActionPlayCard{Cards: []deck.Card{{Rank: deck.King, Suit: deck.Hearts}}}

		err := rules.PreActionCondition(state, action)
		assert.NoError(t, err)
	})

	t.Run("valid eight wildcard", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		rules := &CrazyEightsRules{}
		action := ActionPlayCard{Cards: []deck.Card{{Rank: deck.Eight, Suit: deck.Diamonds}}}

		err := rules.PreActionCondition(state, action)
		assert.NoError(t, err)
	})

	t.Run("invalid mismatch", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		rules := &CrazyEightsRules{}
		action := ActionPlayCard{Cards: []deck.Card{{Rank: deck.King, Suit: deck.Hearts}}}

		err := rules.PreActionCondition(state, action)
		assert.ErrorContains(t, err, "card doesn't match top discard")
	})

	t.Run("invalid card not in hand", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		rules := &CrazyEightsRules{}
		action := ActionPlayCard{Cards: []deck.Card{{Rank: deck.Ace, Suit: deck.Spades}}}

		err := rules.PreActionCondition(state, action)
		assert.ErrorContains(t, err, "you don't have that card")
	})
}

func TestCrazyEightsRules_ApplyAction(t *testing.T) {
	t.Parallel()

	t.Run("card that matches rank or suit", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		rules := &CrazyEightsRules{}
		action := ActionPlayCard{Cards: []deck.Card{{Rank: deck.Two, Suit: deck.Spades}}}

		rules.ApplyAction(state, action)

		extra := state.Extra.(*State)

		assert.Equal(t, deck.Spades, extra.CurrentSuit)
		assert.Len(t, state.Players[0].Cards, 2)

		top, _ := state.Discard.Peak()
		assert.Equal(t, deck.Two, top.Rank)
		assert.Equal(t, deck.Spades, top.Suit)
	})

	t.Run("card that is 8, currentsuit should change, discard shoult change", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		state.Players[0].Cards = []deck.Card{{Rank: deck.Eight, Suit: deck.Diamonds}, {Rank: deck.King, Suit: deck.Hearts}}
		rules := &CrazyEightsRules{}
		action := ActionPlayCard{Cards: []deck.Card{{Rank: deck.Eight, Suit: deck.Diamonds}}, Suit: deck.Clubs}

		rules.ApplyAction(state, action)

		extra := state.Extra.(*State)

		assert.Equal(t, deck.Clubs, extra.CurrentSuit)
		assert.Len(t, state.Players[0].Cards, 1)
	})

	t.Run("draw card", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		rules := &CrazyEightsRules{}
		action := ActionDrawCard{}

		initialSize := state.Deck.Size()
		rules.ApplyAction(state, action)

		assert.Len(t, state.Players[0].Cards, 4)
		assert.Equal(t, initialSize-1, state.Deck.Size())
	})
}

func TestCrazyEightsRules_CheckWinCondition(t *testing.T) {
	t.Parallel()
	rules := &CrazyEightsRules{}

	state := createTestState()
	assert.False(t, rules.CheckWinCondition(state))

	state.Players[0].Cards = []deck.Card{}
	assert.True(t, rules.CheckWinCondition(state))
}

func TestCrazyEightsRules_Init(t *testing.T) {
	t.Parallel()
	rules := &CrazyEightsRules{}

	assert.Equal(t, 7, rules.InitialDealCount())

	d := rules.InitialDeck()
	assert.Len(t, d, 52)

	state := createTestState()
	err := rules.OnGameStart(state)
	assert.NoError(t, err)

	extra := state.Extra.(*State)
	assert.NotNil(t, extra)

	top, ok := state.Discard.Peak()
	assert.True(t, ok)
	assert.Equal(t, top.Suit, extra.CurrentSuit)
}
