package uno

import (
	"bytes"
	"fmt"
	"log/slog"
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

	players := make([]*player.Player, 0, len(hands))
	for i, n := range hands {
		cards, ok := stock.DrawNCards(n)
		require.True(t, ok, "fixture deck must hold %d cards", n)
		players = append(players, &player.Player{ID: fmt.Sprintf("p%d", i+1), Cards: cards})
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
	state := createMultiplayerState(t, 4, 4)
	before := cardsInPlay(state)
	stockBefore := state.Deck.Size()

	(&Rules{}).OnPlayerLeave(state, "p2")

	assert.Equal(t, before, cardsInPlay(state))
	assert.Equal(t, stockBefore+4, state.Deck.Size())
}

func TestRules_OnPlayerLeave_UnknownPlayerChangesNothing(t *testing.T) {
	t.Parallel()
	state := createMultiplayerState(t, 3, 3)
	before := cardsInPlay(state)
	(&Rules{}).OnPlayerLeave(state, "nobody")
	assert.Equal(t, before, cardsInPlay(state))
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
		players := []*player.Player{{ID: "a"}, {ID: "b"}}
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
	players := []*player.Player{{ID: "a"}, {ID: "b"}, {ID: "c"}}
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
