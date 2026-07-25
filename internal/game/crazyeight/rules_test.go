package crazyeight

import (
	"testing"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/player"

	"github.com/stretchr/testify/assert"
)

func createTestState() *game.State {
	rules := &Rules{}
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

func TestRules_PreActionCondition_PlayCard(t *testing.T) {
	t.Parallel()

	t.Run("valid matching suit", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		rules := &Rules{}
		action := ActionPlayCard{Cards: []deck.Card{{Rank: deck.Two, Suit: deck.Spades}}}

		err := rules.PreActionCondition(state, action)
		assert.NoError(t, err)
	})

	t.Run("valid matching rank", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		state.Discard = deck.New([]deck.Card{{Rank: deck.King, Suit: deck.Clubs}})
		rules := &Rules{}
		action := ActionPlayCard{Cards: []deck.Card{{Rank: deck.King, Suit: deck.Hearts}}}

		err := rules.PreActionCondition(state, action)
		assert.NoError(t, err)
	})

	t.Run("valid eight wildcard", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		rules := &Rules{}
		action := ActionPlayCard{Cards: []deck.Card{{Rank: deck.Eight, Suit: deck.Diamonds}}}

		err := rules.PreActionCondition(state, action)
		assert.NoError(t, err)
	})

	t.Run("invalid mismatch", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		rules := &Rules{}
		action := ActionPlayCard{Cards: []deck.Card{{Rank: deck.King, Suit: deck.Hearts}}}

		err := rules.PreActionCondition(state, action)
		assert.ErrorContains(t, err, "card doesn't match top discard")
	})

	t.Run("invalid card not in hand", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		rules := &Rules{}
		action := ActionPlayCard{Cards: []deck.Card{{Rank: deck.Ace, Suit: deck.Spades}}}

		err := rules.PreActionCondition(state, action)
		assert.ErrorContains(t, err, "you don't have that card")
	})
}

func TestRules_ApplyAction(t *testing.T) {
	t.Parallel()

	t.Run("card that matches rank or suit", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		rules := &Rules{}
		action := ActionPlayCard{Cards: []deck.Card{{Rank: deck.Two, Suit: deck.Spades}}}

		rules.ApplyAction(state, action)

		extra := state.Extra.(*State)

		assert.Equal(t, deck.Spades, extra.CurrentSuit)
		assert.Len(t, state.Players[0].Cards, 2)

		top, _ := state.Discard.Peek()
		assert.Equal(t, deck.Two, top.Rank)
		assert.Equal(t, deck.Spades, top.Suit)
	})

	t.Run("card that is 8, currentsuit should change, discard shoult change", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		state.Players[0].Cards = []deck.Card{{Rank: deck.Eight, Suit: deck.Diamonds}, {Rank: deck.King, Suit: deck.Hearts}}
		rules := &Rules{}
		action := ActionPlayCard{Cards: []deck.Card{{Rank: deck.Eight, Suit: deck.Diamonds}}, Suit: deck.Clubs}

		rules.ApplyAction(state, action)

		extra := state.Extra.(*State)

		assert.Equal(t, deck.Clubs, extra.CurrentSuit)
		assert.Len(t, state.Players[0].Cards, 1)
	})

	t.Run("draw card", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		rules := &Rules{}
		action := ActionDrawCard{}

		initialSize := state.Deck.Size()
		rules.ApplyAction(state, action)

		assert.Len(t, state.Players[0].Cards, 4)
		assert.Equal(t, initialSize-1, state.Deck.Size())
	})
}

func TestRules_DrawCard_Reshuffle(t *testing.T) {
	t.Parallel()

	t.Run("empty deck reshuffles discard so draw is allowed and conserves cards", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		rules := &Rules{}

		// Deck empty, discard holds several cards; the top card must stay in
		// play while the rest can refill the stock.
		state.Deck = deck.New([]deck.Card{})
		state.Discard = deck.New([]deck.Card{
			{Rank: deck.Nine, Suit: deck.Spades},
			{Rank: deck.Three, Suit: deck.Hearts},
			{Rank: deck.Jack, Suit: deck.Clubs},
			{Rank: deck.Four, Suit: deck.Diamonds},
		})

		handBefore := len(state.Players[0].Cards)
		totalBefore := handBefore + state.Deck.Size() + state.Discard.Size()

		action := ActionDrawCard{}

		// The discard pile can refill the stock, so the draw is legal.
		err := rules.PreActionCondition(state, action)
		assert.NoError(t, err)

		rules.ApplyAction(state, action)

		// Drawing player gained exactly one card.
		assert.Len(t, state.Players[0].Cards, handBefore+1)

		// Discard was reduced to just its top card.
		assert.Equal(t, 1, state.Discard.Size())

		// No cards were created or lost.
		totalAfter := len(state.Players[0].Cards) + state.Deck.Size() + state.Discard.Size()
		assert.Equal(t, totalBefore, totalAfter)
	})

	t.Run("exhausted board turns draw into a pass and ends the hand", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		rules := &Rules{}
		extra := state.Extra.(*State)

		state.Deck = deck.New([]deck.Card{})
		state.Discard = deck.New([]deck.Card{{Rank: deck.Nine, Suit: deck.Spades}})

		// Draw stays legal with nothing to draw; it becomes a forced pass.
		assert.NoError(t, rules.PreActionCondition(state, ActionDrawCard{}))

		for range state.Players {
			rules.ApplyAction(state, ActionDrawCard{})
		}
		assert.GreaterOrEqual(t, extra.Passes, len(state.Players))
		assert.True(t, rules.CheckWinCondition(state), "a deadlocked board must end the hand")
	})
}

func TestRules_PlayEight_SuitSelection(t *testing.T) {
	t.Parallel()

	t.Run("eight without a suit is rejected", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		rules := &Rules{}
		action := ActionPlayCard{
			Cards: []deck.Card{{Rank: deck.Eight, Suit: deck.Diamonds}},
			Suit:  deck.NoSuit,
		}

		err := rules.PreActionCondition(state, action)
		assert.ErrorContains(t, err, "must choose a suit when playing an eight")
	})

	t.Run("eight with a valid suit is allowed and updates CurrentSuit", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		rules := &Rules{}
		action := ActionPlayCard{
			Cards: []deck.Card{{Rank: deck.Eight, Suit: deck.Diamonds}},
			Suit:  deck.Hearts,
		}

		err := rules.PreActionCondition(state, action)
		assert.NoError(t, err)

		rules.ApplyAction(state, action)

		extra := state.Extra.(*State)
		assert.Equal(t, deck.Hearts, extra.CurrentSuit)
	})
}

func TestRules_CheckWinCondition(t *testing.T) {
	t.Parallel()
	rules := &Rules{}

	state := createTestState()
	assert.False(t, rules.CheckWinCondition(state))

	state.Players[0].Cards = []deck.Card{}
	assert.True(t, rules.CheckWinCondition(state))
}

func TestRules_Init(t *testing.T) {
	t.Parallel()
	rules := &Rules{}

	assert.Equal(t, 7, rules.InitialDealCount())

	d := rules.InitialDeck()
	assert.Len(t, d, 52)

	state := createTestState()
	err := rules.OnGameStart(state)
	assert.NoError(t, err)

	extra := state.Extra.(*State)
	assert.NotNil(t, extra)

	top, ok := state.Discard.Peek()
	assert.True(t, ok)
	assert.Equal(t, top.Suit, extra.CurrentSuit)
}
