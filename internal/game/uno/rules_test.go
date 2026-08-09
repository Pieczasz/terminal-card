package uno

import (
	"bytes"
	"fmt"
	"log/slog"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestState() *game.State {
	rules := &Rules{}
	players := []*game.Player{{ID: "p1", Cards: []deck.Card{
		{Rank: deck.Two, Suit: ColorRed},
		{Rank: deck.Five, Suit: ColorBlue},
		{Rank: Skip, Suit: ColorYellow},
		{Rank: Wild, Suit: ColorWild},
	}}}
	state := game.NewState(rules, players, InitialDeck())
	state.Extra = &State{CurrentColor: ColorRed, Direction: 1}
	state.Discard = deck.New([]deck.Card{{Rank: deck.Three, Suit: ColorRed}})
	state.CurrentTurn = 0
	return state
}

func createMultiplayerState(t *testing.T, hands ...int) *game.State {
	t.Helper()
	rules := &Rules{}
	stock := deck.New(InitialDeck())
	require.NoError(t, stock.Shuffle())

	players := make([]*game.Player, 0, len(hands))
	for i, n := range hands {
		cards, ok := stock.DrawNCards(n)
		require.True(t, ok, "fixture deck must hold %d cards", n)
		players = append(players, &game.Player{ID: fmt.Sprintf("p%d", i+1), Cards: cards})
	}

	top, ok := stock.Draw()
	require.True(t, ok)
	for isWild(top.Rank) {
		stock.AddCard(top)
		require.NoError(t, stock.Shuffle())
		top, ok = stock.Draw()
		require.True(t, ok)
	}

	state := game.NewState(rules, players, nil)
	state.Deck = stock
	state.Discard = deck.New([]deck.Card{top})
	state.Extra = &State{CurrentColor: top.Suit, Direction: 1}
	state.CurrentTurn = 0
	return state
}

func cardsInPlay(state *game.State) int {
	total := state.Deck.Size() + state.Discard.Size()
	for _, p := range state.Players {
		total += len(p.Cards)
	}
	return total
}

func TestRules_ValidateAction_PlayCard(t *testing.T) {
	t.Parallel()
	rules := &Rules{}

	t.Run("matching color", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		require.NoError(t, rules.ValidateAction(state, ActionPlayCard{Card: deck.Card{Rank: deck.Two, Suit: ColorRed}}))
	})

	t.Run("matching number", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		state.Discard = deck.New([]deck.Card{{Rank: deck.Five, Suit: ColorGreen}})
		state.Extra.(*State).CurrentColor = ColorGreen
		require.NoError(t, rules.ValidateAction(state, ActionPlayCard{Card: deck.Card{Rank: deck.Five, Suit: ColorBlue}}))
	})

	t.Run("matching symbol", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		state.Discard = deck.New([]deck.Card{{Rank: Skip, Suit: ColorBlue}})
		state.Extra.(*State).CurrentColor = ColorBlue
		require.NoError(t, rules.ValidateAction(state, ActionPlayCard{Card: deck.Card{Rank: Skip, Suit: ColorYellow}}))
	})

	t.Run("wild always legal with color", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		require.NoError(t, rules.ValidateAction(state, ActionPlayCard{
			Card:        deck.Card{Rank: Wild, Suit: ColorWild},
			ChosenColor: ColorBlue,
		}))
	})

	t.Run("wild without color rejected", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		err := rules.ValidateAction(state, ActionPlayCard{
			Card:        deck.Card{Rank: Wild, Suit: ColorWild},
			ChosenColor: ColorWild, // NoSuit sentinel, same as crazy eights
		})
		require.ErrorContains(t, err, "must choose a valid color")
	})

	t.Run("invalid mismatch", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		err := rules.ValidateAction(state, ActionPlayCard{Card: deck.Card{Rank: deck.Five, Suit: ColorBlue}})
		require.ErrorContains(t, err, "card doesn't match")
	})

	t.Run("card not in hand", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		err := rules.ValidateAction(state, ActionPlayCard{Card: deck.Card{Rank: deck.Nine, Suit: ColorRed}})
		require.ErrorContains(t, err, "you don't have that card")
	})
}

// A Wild Draw Four is the one card whose legality depends on the rest of the hand:
// it may only be played by someone with nothing of the current colour to play. Left
// unchecked it is simply the strongest card in the deck and there is no reason ever
// to play anything else.
func TestRules_ValidateAction_WildDrawFourNeedsNoCurrentColor(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	wd4 := deck.Card{Rank: WildDrawFour, Suit: ColorWild}

	t.Run("rejected while a card of the current color is held", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		state.Players[0].Cards = []deck.Card{wd4, {Rank: deck.Two, Suit: ColorRed}}
		state.Extra.(*State).CurrentColor = ColorRed

		err := rules.ValidateAction(state, ActionPlayCard{Card: wd4, ChosenColor: ColorBlue})
		require.ErrorContains(t, err, "no card of the current color")
	})

	t.Run("allowed once nothing matches the current color", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		state.Players[0].Cards = []deck.Card{wd4, {Rank: deck.Two, Suit: ColorGreen}}
		state.Extra.(*State).CurrentColor = ColorRed

		require.NoError(t, rules.ValidateAction(state, ActionPlayCard{Card: wd4, ChosenColor: ColorBlue}))
	})

	t.Run("a matching number or symbol does not block it", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		// Playable on rank alone, but the gate is on colour, not on playability.
		state.Discard = deck.New([]deck.Card{{Rank: deck.Two, Suit: ColorRed}})
		state.Players[0].Cards = []deck.Card{wd4, {Rank: deck.Two, Suit: ColorGreen}}
		state.Extra.(*State).CurrentColor = ColorRed

		require.NoError(t, rules.ValidateAction(state, ActionPlayCard{Card: wd4, ChosenColor: ColorBlue}))
	})

	t.Run("a held wild is not a card of the current color", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		state.Players[0].Cards = []deck.Card{wd4, {Rank: Wild, Suit: ColorWild}}
		state.Extra.(*State).CurrentColor = ColorRed

		require.NoError(t, rules.ValidateAction(state, ActionPlayCard{Card: wd4, ChosenColor: ColorBlue}))
	})

	t.Run("a plain wild is still unconditional", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		state.Players[0].Cards = []deck.Card{{Rank: Wild, Suit: ColorWild}, {Rank: deck.Two, Suit: ColorRed}}
		state.Extra.(*State).CurrentColor = ColorRed

		require.NoError(t, rules.ValidateAction(state, ActionPlayCard{
			Card:        deck.Card{Rank: Wild, Suit: ColorWild},
			ChosenColor: ColorBlue,
		}))
	})
}

// The card that starts the discard pile is played by the deck, and its action still
// happens: it lands on the seat the engine put on turn. Ignoring it lets the first
// player play on a Skip or a Draw Two as though it were a plain number.
func TestRules_OnGameStart_OpeningCardActsOnTheFirstPlayer(t *testing.T) {
	t.Parallel()

	// Draw takes from the end, so the last card is the one that opens the pile.
	openOn := func(t *testing.T, seats int, card deck.Card) (*game.State, *State) {
		t.Helper()
		players := make([]*game.Player, seats)
		for i := range players {
			players[i] = &game.Player{ID: fmt.Sprintf("p%d", i+1)}
		}
		stock := make([]deck.Card, 0, 12)
		for range 10 {
			stock = append(stock, deck.Card{Rank: deck.Seven, Suit: ColorGreen})
		}
		stock = append(stock, card)

		state := game.NewState(&Rules{}, players, nil)
		state.Deck = deck.New(stock)
		state.CurrentTurn = 0
		require.NoError(t, (&Rules{}).OnGameStart(state))
		return state, state.Extra.(*State)
	}

	t.Run("skip costs the first player their turn", func(t *testing.T) {
		t.Parallel()
		state, _ := openOn(t, 3, deck.Card{Rank: Skip, Suit: ColorRed})

		assert.Equal(t, 1, state.CurrentTurn)
		require.NotNil(t, state.OverrideNextTurn)
		assert.Equal(t, 1, *state.OverrideNextTurn, "the engine settles the cursor after OnGameStart")
	})

	t.Run("draw two is drawn by the first player, who is then skipped", func(t *testing.T) {
		t.Parallel()
		state, _ := openOn(t, 3, deck.Card{Rank: DrawTwo, Suit: ColorRed})

		assert.Len(t, state.Players[0].Cards, 2, "the seat on turn pays the penalty")
		assert.Empty(t, state.Players[1].Cards)
		assert.Equal(t, 1, state.CurrentTurn)
	})

	t.Run("reverse turns the table around before the first play", func(t *testing.T) {
		t.Parallel()
		state, extra := openOn(t, 3, deck.Card{Rank: Reverse, Suit: ColorRed})

		assert.Equal(t, int8(-1), extra.Direction)
		assert.Equal(t, 0, state.CurrentTurn, "a reverse skips nobody")
	})

	t.Run("heads-up a reverse is a skip", func(t *testing.T) {
		t.Parallel()
		state, extra := openOn(t, 2, deck.Card{Rank: Reverse, Suit: ColorRed})

		assert.Equal(t, int8(1), extra.Direction, "with two seats there is nothing to reverse")
		assert.Equal(t, 1, state.CurrentTurn)
	})

	t.Run("a number card leaves the first player on turn", func(t *testing.T) {
		t.Parallel()
		state, extra := openOn(t, 3, deck.Card{Rank: deck.Five, Suit: ColorRed})

		assert.Equal(t, 0, state.CurrentTurn)
		assert.Equal(t, int8(1), extra.Direction)
		assert.Empty(t, state.Players[0].Cards)
	})
}

func TestRules_ApplyAction_NumberCard(t *testing.T) {
	t.Parallel()
	state := createTestState()
	rules := &Rules{}
	card := deck.Card{Rank: deck.Two, Suit: ColorRed}

	rules.ApplyAction(state, ActionPlayCard{Card: card})

	extra := state.Extra.(*State)
	assert.Equal(t, ColorRed, extra.CurrentColor)
	assert.Len(t, state.Players[0].Cards, 3)
	require.NotNil(t, state.OverrideNextTurn)
	assert.Equal(t, 0, *state.OverrideNextTurn) // single player: advance wraps
	top, _ := state.Discard.Peek()
	assert.Equal(t, card, top)
}

func TestRules_ApplyAction_Skip(t *testing.T) {
	t.Parallel()
	state := createMultiplayerState(t, 3, 3, 3)
	rules := &Rules{}
	state.CurrentTurn = 0
	state.Players[0].Cards = []deck.Card{{Rank: Skip, Suit: ColorRed}}
	state.Extra.(*State).CurrentColor = ColorRed
	state.Discard = deck.New([]deck.Card{{Rank: deck.Two, Suit: ColorRed}})

	rules.ApplyAction(state, ActionPlayCard{Card: deck.Card{Rank: Skip, Suit: ColorRed}})

	require.NotNil(t, state.OverrideNextTurn)
	assert.Equal(t, 2, *state.OverrideNextTurn, "skip advances two seats")
}

func TestRules_ApplyAction_Reverse(t *testing.T) {
	t.Parallel()
	rules := &Rules{}

	t.Run("three players flips direction", func(t *testing.T) {
		t.Parallel()
		state := createMultiplayerState(t, 2, 2, 2)
		state.CurrentTurn = 0
		state.Players[0].Cards = []deck.Card{{Rank: Reverse, Suit: ColorRed}}
		state.Extra.(*State).CurrentColor = ColorRed
		state.Discard = deck.New([]deck.Card{{Rank: deck.Two, Suit: ColorRed}})

		rules.ApplyAction(state, ActionPlayCard{Card: deck.Card{Rank: Reverse, Suit: ColorRed}})

		extra := state.Extra.(*State)
		assert.Equal(t, int8(-1), extra.Direction)
		require.NotNil(t, state.OverrideNextTurn)
		assert.Equal(t, 2, *state.OverrideNextTurn, "after reverse from seat 0, next is seat 2")
	})

	t.Run("two players acts as skip", func(t *testing.T) {
		t.Parallel()
		state := createMultiplayerState(t, 2, 2)
		state.CurrentTurn = 0
		state.Players[0].Cards = []deck.Card{{Rank: Reverse, Suit: ColorRed}}
		state.Extra.(*State).CurrentColor = ColorRed
		state.Discard = deck.New([]deck.Card{{Rank: deck.Two, Suit: ColorRed}})

		rules.ApplyAction(state, ActionPlayCard{Card: deck.Card{Rank: Reverse, Suit: ColorRed}})

		extra := state.Extra.(*State)
		assert.Equal(t, int8(1), extra.Direction, "direction unchanged in 2-player")
		require.NotNil(t, state.OverrideNextTurn)
		assert.Equal(t, 0, *state.OverrideNextTurn, "same seat plays again")
	})
}

func TestRules_ApplyAction_DrawTwo(t *testing.T) {
	t.Parallel()
	state := createMultiplayerState(t, 1, 1, 1)
	rules := &Rules{}
	state.CurrentTurn = 0
	state.Players[0].Cards = []deck.Card{{Rank: DrawTwo, Suit: ColorRed}}
	state.Extra.(*State).CurrentColor = ColorRed
	state.Discard = deck.New([]deck.Card{{Rank: deck.Two, Suit: ColorRed}})
	victimBefore := len(state.Players[1].Cards)

	rules.ApplyAction(state, ActionPlayCard{Card: deck.Card{Rank: DrawTwo, Suit: ColorRed}})

	assert.Len(t, state.Players[1].Cards, victimBefore+2)
	require.NotNil(t, state.OverrideNextTurn)
	assert.Equal(t, 2, *state.OverrideNextTurn, "victim is skipped")
}

func TestRules_ApplyAction_WildDrawFour(t *testing.T) {
	t.Parallel()
	state := createMultiplayerState(t, 1, 1, 1)
	rules := &Rules{}
	state.CurrentTurn = 0
	state.Players[0].Cards = []deck.Card{{Rank: WildDrawFour, Suit: ColorWild}}
	state.Discard = deck.New([]deck.Card{{Rank: deck.Two, Suit: ColorRed}})
	victimBefore := len(state.Players[1].Cards)

	rules.ApplyAction(state, ActionPlayCard{
		Card:        deck.Card{Rank: WildDrawFour, Suit: ColorWild},
		ChosenColor: ColorBlue,
	})

	assert.Equal(t, ColorBlue, state.Extra.(*State).CurrentColor)
	assert.Len(t, state.Players[1].Cards, victimBefore+4)
	require.NotNil(t, state.OverrideNextTurn)
	assert.Equal(t, 2, *state.OverrideNextTurn)
}

func TestRules_DrawCard_Reshuffle(t *testing.T) {
	t.Parallel()
	rules := &Rules{}

	t.Run("empty deck reshuffles and conserves", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		state.Deck = deck.New([]deck.Card{})
		state.Discard = deck.New([]deck.Card{
			{Rank: deck.Three, Suit: ColorRed},
			{Rank: deck.Four, Suit: ColorBlue},
			{Rank: deck.Five, Suit: ColorGreen},
			{Rank: deck.Six, Suit: ColorYellow},
		})
		handBefore := len(state.Players[0].Cards)
		totalBefore := cardsInPlay(state)

		rules.ApplyAction(state, ActionDrawCard{})

		assert.Len(t, state.Players[0].Cards, handBefore+1)
		assert.Equal(t, 1, state.Discard.Size())
		assert.Equal(t, totalBefore, cardsInPlay(state))
	})

	t.Run("exhausted board is a pass", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		extra := state.Extra.(*State)
		state.Deck = deck.New(nil)
		state.Discard = deck.New([]deck.Card{{Rank: deck.Three, Suit: ColorRed}})

		rules.ApplyAction(state, ActionDrawCard{})
		assert.Equal(t, 1, extra.Passes)
	})
}

func TestRules_CheckWinCondition(t *testing.T) {
	t.Parallel()
	rules := &Rules{}

	t.Run("empty hand wins", func(t *testing.T) {
		t.Parallel()
		state := createMultiplayerState(t, 0, 3)
		assert.True(t, rules.CheckWinCondition(state))
	})

	t.Run("deadlock ends hand", func(t *testing.T) {
		t.Parallel()
		state := createMultiplayerState(t, 2, 2)
		extra := state.Extra.(*State)
		extra.Passes = 2
		assert.True(t, rules.CheckWinCondition(state))
	})

	t.Run("empty table is not a deadlock", func(t *testing.T) {
		t.Parallel()
		state := createMultiplayerState(t)
		extra := state.Extra.(*State)
		extra.Passes = 3
		assert.False(t, rules.CheckWinCondition(state))
	})

	// The negative case: while every seat still holds cards the hand carries on.
	// Without it, "any player has no cards" and "any player has cards" both pass.
	t.Run("a live hand has no winner", func(t *testing.T) {
		t.Parallel()
		state := createMultiplayerState(t, 3, 5, 1)
		assert.False(t, rules.CheckWinCondition(state))
	})
}

// Official Uno never starts on a Wild, so OnGameStart keeps flipping until a coloured
// card surfaces and shuffles the skipped wilds back into the stock.
func TestRules_OnGameStart_NeverOpensOnAWild(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	players := []*game.Player{{ID: "p1"}, {ID: "p2"}}

	// Draw takes from the end, so these are flipped right-to-left: two wilds first.
	stacked := []deck.Card{
		{Rank: deck.Seven, Suit: ColorGreen},
		{Rank: deck.Four, Suit: ColorBlue},
		{Rank: WildDrawFour, Suit: ColorWild},
		{Rank: Wild, Suit: ColorWild},
	}
	state := game.NewState(rules, players, nil)
	state.Deck = deck.New(stacked)

	require.NoError(t, rules.OnGameStart(state))

	top, ok := state.Discard.Peek()
	require.True(t, ok)
	assert.False(t, isWild(top.Rank), "opened on %v", top)
	assert.Equal(t, top.Suit, state.Extra.(*State).CurrentColor)
	assert.Equal(t, len(stacked), state.Deck.Size()+state.Discard.Size(),
		"the skipped wilds go back into the stock")
}

// Passes counts consecutive fruitless turns and ends a deadlocked hand. A draw that
// actually yielded a card has to clear it, or a live hand can still time itself out.
func TestRules_DrawCard_SuccessfulDrawClearsThePassCount(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	state := createMultiplayerState(t, 3, 3)
	extra := state.Extra.(*State)
	extra.Passes = 1
	stockBefore := state.Deck.Size()

	rules.ApplyAction(state, ActionDrawCard{})

	assert.Zero(t, extra.Passes, "a card was drawn, so the hand is not stalled")
	assert.Len(t, state.Players[0].Cards, 4)
	assert.Equal(t, stockBefore-1, state.Deck.Size())
	assert.False(t, rules.CheckWinCondition(state))
}

func TestRules_Standings_RanksByFewestCards(t *testing.T) {
	t.Parallel()
	state := createMultiplayerState(t, 5, 1, 3)
	standings := (&Rules{}).Standings(state)
	require.Len(t, standings, 3)
	assert.Equal(t, "p2", standings[0].ID)
	assert.Equal(t, "p3", standings[1].ID)
	assert.Equal(t, "p1", standings[2].ID)
}

func TestRules_Standings_TiesAreStable(t *testing.T) {
	t.Parallel()
	state := createMultiplayerState(t, 2, 2, 2)
	first := (&Rules{}).Standings(state)
	second := (&Rules{}).Standings(state)
	for i := range first {
		assert.Equal(t, first[i].ID, second[i].ID)
	}
}

func TestRules_OnPlayerLeave_ReturnsCardsToTheStock(t *testing.T) {
	t.Parallel()
	// Deliberately uneven hands: with equal ones, returning the wrong player's cards
	// still balances the deck and the leak goes unnoticed.
	state := createMultiplayerState(t, 3, 6)
	before := cardsInPlay(state)
	stockBefore := state.Deck.Size()

	(&Rules{}).OnPlayerLeave(state, "p2")

	assert.Equal(t, before, cardsInPlay(state), "cards are conserved")
	assert.Equal(t, stockBefore+6, state.Deck.Size(), "the leaver's six cards go back")
	assert.Empty(t, state.Players[1].Cards, "the leaver's hand is emptied")
	assert.Len(t, state.Players[0].Cards, 3, "everyone else keeps their hand")
}

func TestRules_OnPlayerLeave_UnknownPlayerChangesNothing(t *testing.T) {
	t.Parallel()
	state := createMultiplayerState(t, 3, 3)
	before := cardsInPlay(state)
	(&Rules{}).OnPlayerLeave(state, "nobody")
	assert.Equal(t, before, cardsInPlay(state))
}

// Passes is counted against the number of seats, so a leaver who arrives with the
// count part-way up leaves a table that reads as deadlocked without a single seat
// having passed. Their returned cards also refill the stock the count was measuring.
func TestRules_OnPlayerLeave_ClearsStalePasses(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	state := createMultiplayerState(t, 3, 3, 3)
	extra := state.Extra.(*State)

	extra.Passes = 2
	rules.OnPlayerLeave(state, "p3")
	state.Players = state.Players[:2] // the engine drops the seat after the hook

	assert.Zero(t, extra.Passes, "the count measured a table that no longer exists")
	assert.False(t, rules.CheckWinCondition(state), "nobody passed, so nothing is deadlocked")
}

// The stock is rebuilt from the discard on a failed shuffle, and the pile has to go
// back exactly as it was: Peek reads the end, so the card in play must stay last.
func TestRestoreDiscard_KeepsTheCardInPlayOnTop(t *testing.T) {
	t.Parallel()
	top := deck.Card{Rank: deck.Three, Suit: ColorRed}
	rest := []deck.Card{
		{Rank: deck.Four, Suit: ColorBlue},
		{Rank: Skip, Suit: ColorGreen},
	}

	restored := restoreDiscard(rest, top)

	peeked, ok := restored.Peek()
	require.True(t, ok)
	assert.Equal(t, top, peeked, "a rotated pile puts a card nobody played into play")
	assert.Equal(t, len(rest)+1, restored.Size(), "every card comes back")
}

func TestRules_TimeoutAction_AlwaysDraws(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	assert.Equal(t, ActionDrawCard{}, rules.TimeoutAction(nil))
	state := createTestState()
	assert.NoError(t, rules.ValidateAction(state, rules.TimeoutAction(state)))
}

func TestRules_Init(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	assert.Equal(t, 7, rules.InitialDealCount())
	assert.Len(t, rules.InitialDeck(), 108)

	t.Run("opening wild redrawn", func(t *testing.T) {
		t.Parallel()
		players := []*game.Player{{ID: "a"}, {ID: "b"}}
		// Stock top is a wild, then a colored card.
		stock := []deck.Card{
			{Rank: Wild, Suit: ColorWild},
			{Rank: deck.Five, Suit: ColorGreen},
			{Rank: deck.Two, Suit: ColorRed},
		}
		state := game.NewState(rules, players, stock)
		require.NoError(t, rules.OnGameStart(state))

		extra := state.Extra.(*State)
		top, ok := state.Discard.Peek()
		require.True(t, ok)
		assert.False(t, isWild(top.Rank))
		assert.Equal(t, top.Suit, extra.CurrentColor)
		assert.Equal(t, int8(1), extra.Direction)
	})
}

func TestSmoke_FullHandConservesTheDeck(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	players := []*game.Player{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	engine := game.NewEngine(rules, players, InitialDeck())
	require.NoError(t, engine.Start())
	t.Cleanup(engine.Close)

	countCards := func() int {
		var total int
		engine.WithState(func(s *game.State) { total = cardsInPlay(s) })
		return total
	}
	const wantCards = 108
	require.Equal(t, wantCards, countCards())

	for step := range 400 {
		if engine.IsFinished() {
			break
		}
		id := engine.CurrentPlayerID()
		var choice *deck.Card
		engine.WithState(func(s *game.State) {
			hand := s.Players[s.CurrentTurn].Cards
			for _, card := range hand {
				act := ActionPlayCard{Card: card, ChosenColor: ColorRed}
				if rules.ValidateAction(s, act) == nil {
					c := card
					choice = &c
					return
				}
			}
		})

		var err error
		if choice != nil {
			err = engine.SubmitAction(id, ActionPlayCard{Card: *choice, ChosenColor: ColorRed})
		} else {
			err = engine.SubmitAction(id, ActionDrawCard{})
		}
		require.NoError(t, err, "step %d by %s", step, id)
		require.Equal(t, wantCards, countCards(), "cards changed at step %d", step)
	}
	assert.Equal(t, wantCards, countCards())
}

//nolint:paralleltest // slog.SetDefault is process-wide
func TestRules_OnPlayerLeave_NormalLeaveIsNotAnError(t *testing.T) {
	var logged bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { slog.SetDefault(original) })

	state := createMultiplayerState(t, 3, 3)
	(&Rules{}).OnPlayerLeave(state, "p1")
	assert.Empty(t, logged.String())
}
