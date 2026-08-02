package crazyeight

import (
	"fmt"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/player"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestRules_ValidateAction_PlayCard(t *testing.T) {
	t.Parallel()

	t.Run("valid matching suit", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		rules := &Rules{}
		action := ActionPlayCard{Cards: []deck.Card{{Rank: deck.Two, Suit: deck.Spades}}}

		err := rules.ValidateAction(state, action)
		require.NoError(t, err)
	})

	t.Run("valid matching rank", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		state.Discard = deck.New([]deck.Card{{Rank: deck.King, Suit: deck.Clubs}})
		rules := &Rules{}
		action := ActionPlayCard{Cards: []deck.Card{{Rank: deck.King, Suit: deck.Hearts}}}

		err := rules.ValidateAction(state, action)
		require.NoError(t, err)
	})

	t.Run("valid eight wildcard", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		rules := &Rules{}
		action := ActionPlayCard{Cards: []deck.Card{{Rank: deck.Eight, Suit: deck.Diamonds}}}

		err := rules.ValidateAction(state, action)
		require.NoError(t, err)
	})

	t.Run("invalid mismatch", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		rules := &Rules{}
		action := ActionPlayCard{Cards: []deck.Card{{Rank: deck.King, Suit: deck.Hearts}}}

		err := rules.ValidateAction(state, action)
		require.ErrorContains(t, err, "card doesn't match top discard")
	})

	t.Run("invalid card not in hand", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		rules := &Rules{}
		action := ActionPlayCard{Cards: []deck.Card{{Rank: deck.Ace, Suit: deck.Spades}}}

		err := rules.ValidateAction(state, action)
		require.ErrorContains(t, err, "you don't have that card")
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
		err := rules.ValidateAction(state, action)
		require.NoError(t, err)

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
		require.NoError(t, rules.ValidateAction(state, ActionDrawCard{}))

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

		err := rules.ValidateAction(state, action)
		require.ErrorContains(t, err, "must choose a suit when playing an eight")
	})

	t.Run("eight with a valid suit is allowed and updates CurrentSuit", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		rules := &Rules{}
		action := ActionPlayCard{
			Cards: []deck.Card{{Rank: deck.Eight, Suit: deck.Diamonds}},
			Suit:  deck.Hearts,
		}

		err := rules.ValidateAction(state, action)
		require.NoError(t, err)

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
	require.NoError(t, err)

	extra := state.Extra.(*State)
	assert.NotNil(t, extra)

	top, ok := state.Discard.Peek()
	assert.True(t, ok)
	assert.Equal(t, top.Suit, extra.CurrentSuit)
}

// createMultiplayerState is the fixture the multi-seat behaviours need: turn order,
// standings, leave handling and the all-passed deadlock are all unreachable with the
// single-player createTestState above.
func createMultiplayerState(t *testing.T, hands ...int) *game.State {
	t.Helper()
	rules := &Rules{}
	stock := deck.New(deck.StandardDeck())

	players := make([]*player.Player, 0, len(hands))
	for i, n := range hands {
		cards, ok := stock.DrawNCards(n)
		require.True(t, ok, "fixture deck must hold %d cards", n)
		players = append(players, &player.Player{ID: fmt.Sprintf("p%d", i+1), Cards: cards})
	}

	top, ok := stock.Draw()
	require.True(t, ok)

	state := game.NewState(rules, players, nil)
	state.Deck = stock
	state.Discard = deck.New([]deck.Card{top})
	state.Extra = &State{CurrentSuit: top.Suit}
	state.CurrentTurn = 0
	return state
}

// cardsInPlay is the crazy-eights conservation invariant: every card is in exactly
// one of the hands, the stock or the discard.
func cardsInPlay(state *game.State) int {
	total := state.Deck.Size() + state.Discard.Size()
	for _, p := range state.Players {
		total += len(p.Cards)
	}
	return total
}

func TestRules_Standings_RanksByFewestCards(t *testing.T) {
	t.Parallel()
	state := createMultiplayerState(t, 5, 1, 3)

	standings := (&Rules{}).Standings(state)

	require.Len(t, standings, 3)
	assert.Equal(t, "p2", standings[0].ID, "one card is the best position")
	assert.Equal(t, "p3", standings[1].ID)
	assert.Equal(t, "p1", standings[2].ID, "five cards is the worst")
}

// Ties must keep a stable order so two players on the same count do not swap places
// between renders.
func TestRules_Standings_TiesAreStable(t *testing.T) {
	t.Parallel()
	state := createMultiplayerState(t, 2, 2, 2)

	first := (&Rules{}).Standings(state)
	second := (&Rules{}).Standings(state)

	require.Len(t, first, 3)
	for i := range first {
		assert.Equal(t, first[i].ID, second[i].ID, "position %d must not move between calls", i)
	}
}

// A player leaving mid-hand hands their cards back to the stock. If that ever stops
// conserving cards the deck silently shrinks for the rest of the game.
func TestRules_OnPlayerLeave_ReturnsCardsToTheStock(t *testing.T) {
	t.Parallel()
	state := createMultiplayerState(t, 4, 4, 4)
	before := cardsInPlay(state)
	stockBefore := state.Deck.Size()

	(&Rules{}).OnPlayerLeave(state, "p2")

	assert.Equal(t, before, cardsInPlay(state), "leaving must not create or destroy cards")
	assert.Equal(t, stockBefore+4, state.Deck.Size(), "their four cards went back to the stock")

	for _, p := range state.Players {
		if p.ID == "p2" {
			assert.Empty(t, p.Cards, "the leaver keeps no cards")
		}
	}
}

// Leaving with an unknown ID must be a no-op rather than disturbing the table.
func TestRules_OnPlayerLeave_UnknownPlayerChangesNothing(t *testing.T) {
	t.Parallel()
	state := createMultiplayerState(t, 3, 3)
	before := cardsInPlay(state)
	stockBefore := state.Deck.Size()

	(&Rules{}).OnPlayerLeave(state, "nobody")

	assert.Equal(t, before, cardsInPlay(state))
	assert.Equal(t, stockBefore, state.Deck.Size())
}

// With three seats the hand only ends once all three have passed in succession;
// fewer passes must not end it. The single-player fixture made this vacuous.
func TestRules_CheckWinCondition_EndsOnlyWhenEverySeatHasPassed(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	state := createMultiplayerState(t, 3, 3, 3)
	extra, ok := state.Extra.(*State)
	require.True(t, ok)

	for passes := range len(state.Players) {
		extra.Passes = passes
		assert.False(t, rules.CheckWinCondition(state),
			"%d of %d seats passed is not a deadlock", passes, len(state.Players))
	}

	extra.Passes = len(state.Players)
	assert.True(t, rules.CheckWinCondition(state), "every seat passing ends the hand")
}

// An emptied hand still wins outright, regardless of passes.
func TestRules_CheckWinCondition_EmptyHandWins(t *testing.T) {
	t.Parallel()
	state := createMultiplayerState(t, 0, 3, 3)

	assert.True(t, (&Rules{}).CheckWinCondition(state))
}

// A full hand driven through the engine, the crazy-eights counterpart to poker's
// smoke test. The invariant is card conservation: 52 cards exist at every step, no
// matter how many reshuffles or forced passes happen along the way.
func TestSmoke_FullHandConservesTheDeck(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	players := []*player.Player{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	engine := game.NewEngine(rules, players, deck.StandardDeck())
	require.NoError(t, engine.Start())
	t.Cleanup(engine.Close)

	countCards := func() int {
		var total int
		engine.WithState(func(s *game.State) { total = cardsInPlay(s) })
		return total
	}
	const wantCards = 52
	require.Equal(t, wantCards, countCards(), "dealing must not lose cards")

	for step := range 300 {
		if engine.IsFinished() {
			break
		}
		id := engine.CurrentPlayerID()

		// Play a legal card if we hold one, otherwise draw.
		var played bool
		engine.WithState(func(s *game.State) {
			hand := s.Players[s.CurrentTurn].Cards
			for _, card := range hand {
				if rules.ValidateAction(s, ActionPlayCard{Cards: []deck.Card{card}, Suit: deck.Spades}) == nil {
					played = true
					return
				}
			}
		})

		var err error
		if played {
			var choice deck.Card
			engine.WithState(func(s *game.State) {
				for _, card := range s.Players[s.CurrentTurn].Cards {
					if rules.ValidateAction(s, ActionPlayCard{Cards: []deck.Card{card}, Suit: deck.Spades}) == nil {
						choice = card
						return
					}
				}
			})
			err = engine.SubmitAction(id, ActionPlayCard{Cards: []deck.Card{choice}, Suit: deck.Spades})
		} else {
			err = engine.SubmitAction(id, ActionDrawCard{})
		}
		require.NoError(t, err, "step %d by %s", step, id)
		require.Equal(t, wantCards, countCards(), "cards changed at step %d", step)
	}

	assert.Equal(t, wantCards, countCards(), "the finished hand still holds every card")
	assert.NotEmpty(t, engine.StandingsIDs(), "a finished hand ranks its players")
}
