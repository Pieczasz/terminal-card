package ginrummy

import (
	"slices"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func twoPlayers(hands ...[]deck.Card) []*game.Player {
	out := []*game.Player{{ID: "p1"}, {ID: "p2"}}
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

// takeUpcard draws the face-up discard for the seat on turn and returns the card the
// rules must now refuse to take straight back.
func takeUpcard(t *testing.T, rules *Rules) (*game.State, *State, deck.Card) {
	t.Helper()
	state, extra := startedState(t)
	up, ok := state.Discard.Peek()
	require.True(t, ok)
	rules.ApplyAction(state, ActionDrawDiscard{})
	require.NotNil(t, extra.TakenUpcard, "drawing the discard must record the taken upcard")
	require.Equal(t, up, *extra.TakenUpcard)
	return state, extra, up
}

func TestRules_ValidateAction_CannotLayBackTakenUpcard(t *testing.T) {
	t.Parallel()
	rules := &Rules{}

	t.Run("discarding it straight back is refused", func(t *testing.T) {
		t.Parallel()
		state, _, up := takeUpcard(t, rules)
		err := rules.ValidateAction(state, ActionDiscard{Card: up})
		require.ErrorContains(t, err, "just took from the discard pile")
	})

	t.Run("knocking with it is refused", func(t *testing.T) {
		t.Parallel()
		state, _, up := takeUpcard(t, rules)
		err := rules.ValidateAction(state, ActionKnock{Discard: up})
		require.ErrorContains(t, err, "just took from the discard pile")
	})

	t.Run("every other card in hand is still legal", func(t *testing.T) {
		t.Parallel()
		state, _, up := takeUpcard(t, rules)
		for _, card := range state.Players[state.CurrentTurn].Cards {
			if card == up {
				continue
			}
			require.NoError(t, rules.ValidateAction(state, ActionDiscard{Card: card}))
		}
	})

	t.Run("a stock draw carries no restriction", func(t *testing.T) {
		t.Parallel()
		state, extra := startedState(t)
		rules.ApplyAction(state, ActionDrawStock{})
		assert.Nil(t, extra.TakenUpcard)
		for _, card := range state.Players[state.CurrentTurn].Cards {
			require.NoError(t, rules.ValidateAction(state, ActionDiscard{Card: card}))
		}
	})

	t.Run("the restriction ends with the turn", func(t *testing.T) {
		t.Parallel()
		state, extra, up := takeUpcard(t, rules)
		var other deck.Card
		for _, card := range state.Players[state.CurrentTurn].Cards {
			if card != up {
				other = card
				break
			}
		}
		rules.ApplyAction(state, ActionDiscard{Card: other})
		assert.Nil(t, extra.TakenUpcard, "the next player may take what was just laid down")
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

// The undercut boundary is `remPts <= knockerPts`, so a tie goes to the opponent.
// With `<` the knocker would win the hand instead, for zero points.
func TestRules_Knock_UndercutOnATie(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	// Three melds plus 5♦ deadwood; the knock discards K♣, leaving 5 points.
	knocker := []deck.Card{
		c(deck.Two, deck.Hearts), c(deck.Three, deck.Hearts), c(deck.Four, deck.Hearts),
		c(deck.Jack, deck.Spades), c(deck.Jack, deck.Hearts), c(deck.Jack, deck.Diamonds),
		c(deck.Ace, deck.Clubs), c(deck.Ace, deck.Spades), c(deck.Ace, deck.Hearts),
		c(deck.Five, deck.Diamonds),
		c(deck.King, deck.Clubs),
	}
	// Three melds plus 5♣: also 5 points, and it lays off onto none of the knocker's
	// melds (wrong suit for the heart run, wrong rank for both sets).
	opponent := []deck.Card{
		c(deck.Six, deck.Spades), c(deck.Seven, deck.Spades), c(deck.Eight, deck.Spades),
		c(deck.King, deck.Hearts), c(deck.King, deck.Diamonds), c(deck.King, deck.Spades),
		c(deck.Queen, deck.Clubs), c(deck.Queen, deck.Hearts), c(deck.Queen, deck.Diamonds),
		c(deck.Five, deck.Clubs),
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

	result := extra.LastHandResult
	require.NotNil(t, result)
	require.Equal(t, 5, result.KnockerDeadwoodPoints)
	require.Equal(t, 5, result.OpponentDeadwoodPoints, "the tie is the point of this test")
	assert.True(t, result.Undercut, "equal deadwood undercuts the knocker")
	assert.Equal(t, "p2", result.Winner)
	assert.Equal(t, UndercutBonus, result.ScoreDelta, "a tie scores the bonus alone")
	assert.Equal(t, UndercutBonus, extra.CumulativeScores["p2"])
	assert.Zero(t, extra.CumulativeScores["p1"])
}

// WallStockSize cards stay undealt, so the boundary is drawn at exactly that size.
func TestRules_DrawStock_WallBoundary(t *testing.T) {
	t.Parallel()
	rules := &Rules{}

	tests := []struct {
		name    string
		stock   int
		wantErr bool
	}{
		{name: "one above the wall is drawable", stock: WallStockSize + 1},
		{name: "exactly at the wall is not", stock: WallStockSize, wantErr: true},
		{name: "below the wall is not", stock: WallStockSize - 1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			state, _ := startedState(t)
			state.Deck = deck.New(deck.StandardDeck()[:tt.stock])

			err := rules.ValidateAction(state, ActionDrawStock{})
			if tt.wantErr {
				require.ErrorContains(t, err, "stock is at the wall")
				return
			}
			require.NoError(t, err)
		})
	}
}

// An absent player must still be handed a legal move at the wall, where the stock is
// off limits and the discard pile is the only place left to draw from.
func TestRules_TimeoutAction_DrawsTheDiscardAtTheWall(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	state, _ := startedState(t)
	state.Deck = deck.New(deck.StandardDeck()[:WallStockSize])

	action := rules.TimeoutAction(state)
	assert.Equal(t, ActionDrawDiscard{}, action)
	require.NoError(t, rules.ValidateAction(state, action), "the safe move must be legal")
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

// The engine removes a player whose auto-play the rules then refuse, so the move
// TimeoutAction picks must satisfy ValidateAction — including the upcard restriction.
func TestRules_TimeoutAction_NeverLaysBackTakenUpcard(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	state, extra, up := takeUpcard(t, rules)

	action := rules.TimeoutAction(state)
	require.NotNil(t, action)
	discard, ok := action.(ActionDiscard)
	require.True(t, ok, "awaiting a discard, got %T", action)
	assert.NotEqual(t, up, discard.Card)
	assert.NotNil(t, extra.TakenUpcard)
	require.NoError(t, rules.ValidateAction(state, action), "the safe move must be legal")
}

func TestAutoDiscard(t *testing.T) {
	t.Parallel()
	king, twoD := c(deck.King, deck.Clubs), c(deck.Two, deck.Diamonds)
	// Three melds plus two loose cards: K♣ (10 pts) and 2♦ (2 pts).
	hand := []deck.Card{
		c(deck.Two, deck.Hearts), c(deck.Three, deck.Hearts), c(deck.Four, deck.Hearts),
		c(deck.Jack, deck.Spades), c(deck.Jack, deck.Hearts), c(deck.Jack, deck.Diamonds),
		c(deck.Ace, deck.Clubs), c(deck.Ace, deck.Spades), c(deck.Ace, deck.Hearts),
		king, twoD,
	}
	gin := []deck.Card{
		c(deck.Two, deck.Hearts), c(deck.Three, deck.Hearts), c(deck.Four, deck.Hearts),
		c(deck.Five, deck.Hearts), c(deck.Six, deck.Hearts),
		c(deck.Jack, deck.Spades), c(deck.Jack, deck.Hearts), c(deck.Jack, deck.Diamonds),
		c(deck.Ace, deck.Clubs), c(deck.Ace, deck.Spades), c(deck.Ace, deck.Hearts),
	}

	tests := []struct {
		name      string
		hand      []deck.Card
		forbidden *deck.Card
		want      deck.Card
	}{
		{name: "sheds the priciest deadwood", hand: hand, want: king},
		{name: "skips the forbidden card", hand: hand, forbidden: &king, want: twoD},
		{name: "cheap deadwood still loses to expensive", hand: hand, forbidden: &twoD, want: king},
		{name: "gin breaks a meld rather than stalling", hand: gin, want: gin[0]},
		{name: "gin skips the forbidden card too", hand: gin, forbidden: &gin[0], want: gin[1]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := autoDiscard(slices.Clone(tt.hand), tt.forbidden)
			require.True(t, ok)
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("a single forbidden card leaves nothing to play", func(t *testing.T) {
		t.Parallel()
		_, ok := autoDiscard([]deck.Card{king}, &king)
		assert.False(t, ok)
	})
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
