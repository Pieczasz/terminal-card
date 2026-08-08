package ginrummy

import (
	"testing"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/player"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func twoPlayers(hands ...[]deck.Card) []*player.Player {
	out := []*player.Player{{ID: "p1"}, {ID: "p2"}}
	for i, h := range hands {
		out[i].Cards = h
	}
	return out
}

func startedState(t *testing.T) (*game.State, *State) {
	t.Helper()
	rules := &Rules{}
	players := twoPlayers()
	state := game.NewState(rules, players, nil)
	require.NoError(t, rules.OnGameStart(state))
	extra := state.Extra.(*State)
	return state, extra
}

func TestRules_OnGameStart_DealsTen(t *testing.T) {
	t.Parallel()
	state, extra := startedState(t)
	assert.Equal(t, 1, extra.HandNumber)
	assert.Equal(t, AwaitingDraw, extra.HandPhase)
	assert.Len(t, state.Players[0].Cards, 10)
	assert.Len(t, state.Players[1].Cards, 10)
	assert.Equal(t, 31, state.Deck.Size())
	_, ok := state.Discard.Peek()
	assert.True(t, ok)
}

func TestRules_ValidateAction_PhaseGates(t *testing.T) {
	t.Parallel()
	rules := &Rules{}

	t.Run("discard rejected before draw", func(t *testing.T) {
		t.Parallel()
		state, _ := startedState(t)
		card := state.Players[0].Cards[0]
		err := rules.ValidateAction(state, ActionDiscard{Card: card})
		require.ErrorContains(t, err, "must draw first")
	})

	t.Run("draw stock accepted", func(t *testing.T) {
		t.Parallel()
		state, _ := startedState(t)
		err := rules.ValidateAction(state, ActionDrawStock{})
		require.NoError(t, err)
	})

	t.Run("draw rejected after draw", func(t *testing.T) {
		t.Parallel()
		state, extra := startedState(t)
		rules.ApplyAction(state, ActionDrawStock{})
		err := rules.ValidateAction(state, ActionDrawStock{})
		require.ErrorContains(t, err, "must discard first")
		assert.Equal(t, AwaitingDiscard, extra.HandPhase)
	})
}

func TestRules_ValidateAction_KnockBoundary(t *testing.T) {
	t.Parallel()
	rules := &Rules{}

	t.Run("deadwood 10 legal", func(t *testing.T) {
		t.Parallel()
		hand11 := []deck.Card{
			c(deck.Ace, deck.Spades), c(deck.Ace, deck.Hearts), c(deck.Ace, deck.Diamonds),
			c(deck.Four, deck.Clubs), c(deck.Five, deck.Clubs), c(deck.Six, deck.Clubs),
			c(deck.Nine, deck.Hearts), c(deck.Ten, deck.Hearts), c(deck.Jack, deck.Hearts),
			c(deck.King, deck.Spades),
			c(deck.Two, deck.Diamonds),
		}
		state := game.NewState(rules, twoPlayers(hand11, nil), nil)
		state.Extra = &State{HandPhase: AwaitingDiscard, CumulativeScores: map[string]int{"p1": 0, "p2": 0}}
		state.CurrentTurn = 0
		err := rules.ValidateAction(state, ActionKnock{Discard: c(deck.Two, deck.Diamonds)})
		require.NoError(t, err)
	})

	t.Run("deadwood 11 illegal", func(t *testing.T) {
		t.Parallel()
		hand := []deck.Card{
			c(deck.Ace, deck.Spades), c(deck.Ace, deck.Hearts), c(deck.Ace, deck.Diamonds),
			c(deck.Four, deck.Clubs), c(deck.Five, deck.Clubs), c(deck.Six, deck.Clubs),
			c(deck.Nine, deck.Hearts), c(deck.Ten, deck.Hearts), c(deck.Jack, deck.Hearts),
			c(deck.King, deck.Spades),
			c(deck.Queen, deck.Diamonds),
		}
		st := game.NewState(rules, twoPlayers(hand, nil), nil)
		st.Extra = &State{HandPhase: AwaitingDiscard, CumulativeScores: map[string]int{"p1": 0, "p2": 0}}
		st.CurrentTurn = 0
		err := rules.ValidateAction(st, ActionKnock{Discard: c(deck.Ace, deck.Spades)})
		require.ErrorContains(t, err, "deadwood")
	})
}

func TestRules_Knock_Gin(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	knocker := []deck.Card{
		c(deck.Two, deck.Hearts), c(deck.Three, deck.Hearts), c(deck.Four, deck.Hearts), c(deck.Five, deck.Hearts),
		c(deck.Jack, deck.Spades), c(deck.Jack, deck.Hearts), c(deck.Jack, deck.Diamonds),
		c(deck.Ace, deck.Clubs), c(deck.Ace, deck.Spades), c(deck.Ace, deck.Hearts),
		c(deck.King, deck.Clubs), // discard
	}
	opponent := []deck.Card{
		c(deck.King, deck.Spades), c(deck.Queen, deck.Hearts), c(deck.Nine, deck.Diamonds),
		c(deck.Eight, deck.Clubs), c(deck.Seven, deck.Spades), c(deck.Six, deck.Hearts),
		c(deck.Three, deck.Clubs), c(deck.Two, deck.Diamonds), c(deck.Four, deck.Spades),
		c(deck.Five, deck.Diamonds),
	}
	state := game.NewState(rules, twoPlayers(knocker, opponent), nil)
	extra := &State{
		HandPhase:        AwaitingDiscard,
		FirstActor:       0,
		CumulativeScores: map[string]int{"p1": 0, "p2": 0},
	}
	state.Extra = extra
	state.CurrentTurn = 0
	state.Deck = deck.New(nil)
	state.Discard = deck.New(nil)

	require.NoError(t, rules.ValidateAction(state, ActionKnock{Discard: c(deck.King, deck.Clubs)}))
	rules.ApplyAction(state, ActionKnock{Discard: c(deck.King, deck.Clubs)})

	require.NotNil(t, extra.LastHandResult)
	assert.True(t, extra.LastHandResult.Gin)
	assert.Equal(t, "p1", extra.LastHandResult.Winner)
	assert.Equal(t, sumDeadwood(opponent)+GinBonus, extra.LastHandResult.ScoreDelta)
	assert.Empty(t, extra.LastHandResult.LaidOffCards, "gin blocks layoffs")
	assert.Equal(t, extra.LastHandResult.ScoreDelta, extra.CumulativeScores["p1"])
}

func TestRules_Knock_Undercut(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	knocker := []deck.Card{
		c(deck.Two, deck.Hearts), c(deck.Three, deck.Hearts), c(deck.Four, deck.Hearts),
		c(deck.Jack, deck.Spades), c(deck.Jack, deck.Hearts), c(deck.Jack, deck.Diamonds),
		c(deck.Ace, deck.Clubs), c(deck.Ace, deck.Spades), c(deck.Ace, deck.Hearts),
		c(deck.Five, deck.Diamonds), // deadwood 5
		c(deck.King, deck.Clubs),    // discard
	}
	opponent := []deck.Card{
		c(deck.Six, deck.Spades), c(deck.Seven, deck.Spades), c(deck.Eight, deck.Spades), c(deck.Nine, deck.Spades),
		c(deck.King, deck.Hearts), c(deck.King, deck.Diamonds), c(deck.King, deck.Spades),
		c(deck.Queen, deck.Clubs), c(deck.Queen, deck.Hearts), c(deck.Queen, deck.Diamonds),
	}
	state := game.NewState(rules, twoPlayers(knocker, opponent), nil)
	extra := &State{
		HandPhase:        AwaitingDiscard,
		FirstActor:       0,
		CumulativeScores: map[string]int{"p1": 0, "p2": 0},
	}
	state.Extra = extra
	state.CurrentTurn = 0
	state.Deck = deck.New(nil)
	state.Discard = deck.New(nil)

	rules.ApplyAction(state, ActionKnock{Discard: c(deck.King, deck.Clubs)})
	require.NotNil(t, extra.LastHandResult)
	assert.True(t, extra.LastHandResult.Undercut)
	assert.Equal(t, "p2", extra.LastHandResult.Winner)
	assert.Equal(t, 5+UndercutBonus, extra.LastHandResult.ScoreDelta)
	assert.Equal(t, 5+UndercutBonus, extra.CumulativeScores["p2"])
}

func TestRules_Wall_AfterDiscard(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	state, extra := startedState(t)
	state.CurrentTurn = 0
	extra.HandPhase = AwaitingDiscard
	// Stock already at wall size: discard ends the hand.
	state.Deck = deck.New([]deck.Card{
		c(deck.Two, deck.Clubs), c(deck.Three, deck.Clubs),
	})
	card := state.Players[0].Cards[0]
	state.Players[0].Cards = append(state.Players[0].Cards, c(deck.Ace, deck.Diamonds)) // 11 cards
	rules.ApplyAction(state, ActionDiscard{Card: card})
	require.NoError(t, rules.AfterAction(state, ActionDiscard{Card: card}))
	assert.True(t, extra.HandComplete)
	require.NotNil(t, extra.LastHandResult)
	assert.True(t, extra.LastHandResult.Wall)
	assert.Equal(t, 0, extra.CumulativeScores["p1"])
	assert.Equal(t, 0, extra.CumulativeScores["p2"])
}

func TestRules_Wall_StockThreeDoesNotTrigger(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	state, extra := startedState(t)
	extra.HandPhase = AwaitingDiscard
	state.Deck = deck.New([]deck.Card{
		c(deck.Two, deck.Clubs), c(deck.Three, deck.Clubs), c(deck.Four, deck.Clubs),
	})
	card := state.Players[0].Cards[0]
	state.Players[0].Cards = append(state.Players[0].Cards, c(deck.Ace, deck.Diamonds))
	rules.ApplyAction(state, ActionDiscard{Card: card})
	require.NoError(t, rules.AfterAction(state, ActionDiscard{Card: card}))
	assert.False(t, extra.HandComplete)
	assert.Equal(t, AwaitingDraw, extra.HandPhase)
}

func TestRules_Standings_Descending(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	state, extra := startedState(t)
	extra.CumulativeScores["p1"] = 40
	extra.CumulativeScores["p2"] = 90
	standings := rules.Standings(state)
	require.Len(t, standings, 2)
	assert.Equal(t, "p2", standings[0].ID)
	assert.Equal(t, "p1", standings[1].ID)
}

func TestRules_CheckWinCondition(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	state, extra := startedState(t)
	assert.False(t, rules.CheckWinCondition(state))
	extra.MatchComplete = true
	assert.True(t, rules.CheckWinCondition(state))
}

func TestRules_TimeoutAction_DrawStock(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	state, _ := startedState(t)
	assert.Equal(t, ActionDrawStock{}, rules.TimeoutAction(state))
}

func TestRules_TimeoutAction_NextHand(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	state, extra := startedState(t)
	extra.HandComplete = true
	assert.Equal(t, ActionNextHand{}, rules.TimeoutAction(state))
}

func TestRules_TimeoutAction_MatchOver(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	state, extra := startedState(t)
	extra.HandComplete = true
	extra.MatchComplete = true
	assert.Nil(t, rules.TimeoutAction(state))
}

func TestRules_DrawKeepsTurn(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	state, _ := startedState(t)
	state.CurrentTurn = 0
	rules.ApplyAction(state, ActionDrawStock{})
	require.NotNil(t, state.OverrideNextTurn)
	assert.Equal(t, 0, *state.OverrideNextTurn)
	assert.Len(t, state.Players[0].Cards, 11)
}
