package game

import (
	"errors"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/deck"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func shedState(stock, discard []deck.Card) *State {
	return &State{
		Players: []*Player{{ID: "p1", Cards: []deck.Card{{Rank: deck.Ace, Suit: deck.Spades}}}},
		Deck:    deck.New(stock),
		Discard: deck.New(discard),
	}
}

func shedCardsInPlay(state *State) int {
	total := state.Deck.Size() + state.Discard.Size()
	for _, p := range state.Players {
		total += len(p.Cards)
	}
	return total
}

func TestReshuffleDiscardIntoStock(t *testing.T) {
	t.Parallel()
	discard := []deck.Card{
		{Rank: deck.Two, Suit: deck.Hearts},
		{Rank: deck.Three, Suit: deck.Clubs},
		{Rank: deck.Four, Suit: deck.Diamonds},
	}

	t.Run("refills the stock and conserves every card", func(t *testing.T) {
		t.Parallel()
		state := shedState(nil, discard)
		before := shedCardsInPlay(state)
		top, _ := state.Discard.Peek()

		require.NoError(t, ReshuffleDiscardIntoStock(state))

		assert.Equal(t, before, shedCardsInPlay(state))
		assert.Equal(t, 1, state.Discard.Size(), "the card in play stays in play")
		nowTop, _ := state.Discard.Peek()
		assert.Equal(t, top, nowTop)
		assert.Equal(t, len(discard)-1, state.Deck.Size())
	})

	// The failure path throws the stock away, which only conserves cards while the
	// stock was empty. A caller that reshuffles too early would destroy it silently.
	t.Run("refuses a stock that is not empty", func(t *testing.T) {
		t.Parallel()
		state := shedState([]deck.Card{{Rank: deck.Five, Suit: deck.Spades}}, discard)
		before := shedCardsInPlay(state)

		require.ErrorIs(t, ReshuffleDiscardIntoStock(state), errStockNotEmpty)
		assert.Equal(t, before, shedCardsInPlay(state), "a refused reshuffle changes nothing")
		assert.Equal(t, 1, state.Deck.Size())
	})

	t.Run("an empty discard is a no-op", func(t *testing.T) {
		t.Parallel()
		state := shedState(nil, nil)
		require.NoError(t, ReshuffleDiscardIntoStock(state))
		assert.Equal(t, 1, shedCardsInPlay(state))
	})

	t.Run("a failed shuffle conserves the cards and leaves an empty stock", func(t *testing.T) {
		t.Parallel()
		state := shedState(nil, discard)
		before := shedCardsInPlay(state)
		boom := errors.New("no entropy")

		err := reshuffleDiscardIntoStock(state, func(*deck.Pile) error { return boom })

		require.ErrorIs(t, err, boom)
		assert.Equal(t, before, shedCardsInPlay(state), "a failed shuffle must not lose cards")
		assert.True(t, state.Deck.IsEmpty(), "an unshuffled stock never reaches play")
		assert.Equal(t, len(discard), state.Discard.Size(), "the pile goes back as it was")
		top, _ := state.Discard.Peek()
		assert.Equal(t, discard[len(discard)-1], top, "a rotated pile puts an unplayed card into play")
	})
}

func TestRestoreDiscard_KeepsTheCardInPlayOnTop(t *testing.T) {
	t.Parallel()
	top := deck.Card{Rank: deck.Nine, Suit: deck.Spades}
	rest := []deck.Card{
		{Rank: deck.Three, Suit: deck.Hearts},
		{Rank: deck.Jack, Suit: deck.Clubs},
	}

	restored := restoreDiscard(rest, top)

	peeked, ok := restored.Peek()
	require.True(t, ok)
	assert.Equal(t, top, peeked)
	assert.Equal(t, len(rest)+1, restored.Size(), "every card comes back")
}

func TestReturnHandToStock(t *testing.T) {
	t.Parallel()

	t.Run("the leaver's cards go back to the stock", func(t *testing.T) {
		t.Parallel()
		state := shedState([]deck.Card{{Rank: deck.Two, Suit: deck.Clubs}}, nil)
		state.Players = append(state.Players, &Player{ID: "p2", Cards: []deck.Card{
			{Rank: deck.King, Suit: deck.Hearts},
			{Rank: deck.Queen, Suit: deck.Hearts},
		}})
		before := shedCardsInPlay(state)

		ReturnHandToStock(state, "p2", "test")

		assert.Equal(t, before, shedCardsInPlay(state))
		assert.Equal(t, 3, state.Deck.Size())
		assert.Empty(t, state.Players[1].Cards)
		assert.Len(t, state.Players[0].Cards, 1, "everyone else keeps their hand")
	})

	t.Run("an unknown player changes nothing", func(t *testing.T) {
		t.Parallel()
		state := shedState(nil, nil)
		before := shedCardsInPlay(state)
		ReturnHandToStock(state, "nobody", "test")
		assert.Equal(t, before, shedCardsInPlay(state))
	})
}

func TestHandEmptyOrAllPassed(t *testing.T) {
	t.Parallel()
	seated := func(counts ...int) *State {
		state := &State{}
		for i, n := range counts {
			state.Players = append(state.Players, &Player{
				ID:    string(rune('a' + i)),
				Cards: make([]deck.Card, n),
			})
		}
		return state
	}

	tests := []struct {
		name   string
		state  *State
		passes int
		want   bool
	}{
		{name: "an empty hand wins", state: seated(0, 3), want: true},
		{name: "a live hand carries on", state: seated(3, 5, 1), want: false},
		{name: "fewer passes than seats is not a deadlock", state: seated(2, 2, 2), passes: 2},
		{name: "one pass per seat ends the hand", state: seated(2, 2, 2), passes: 3, want: true},
		{name: "an empty table is not a deadlock", state: seated(), passes: 3, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, HandEmptyOrAllPassed(tt.state, tt.passes))
		})
	}
}
