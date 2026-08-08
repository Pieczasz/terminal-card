package poker

import (
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/player"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seat describes one player's betting position for the round-progress helpers, which
// is all any of them read.
type seat struct {
	id     string
	chips  uint
	bet    uint
	acted  bool
	folded bool
	allIn  bool
}

func seatedRound(currentBet uint, seats ...seat) (*game.State, *State) {
	players := make([]*player.Player, 0, len(seats))
	extra := &State{
		CurrentBet:       currentBet,
		BigBlind:         DefaultBigBlind,
		MinRaise:         DefaultBigBlind,
		Phase:            PreFlop,
		Folded:           map[string]bool{},
		PlayersAllIn:     map[string]bool{},
		Table:            make([]deck.Card, 0, 5),
		PlayerChips:      map[string]uint{},
		PlayerBets:       map[string]uint{},
		TotalContributed: map[string]uint{},
		ActedThisRound:   map[string]bool{},
	}
	for _, s := range seats {
		players = append(players, &player.Player{ID: s.id})
		extra.PlayerChips[s.id] = s.chips
		extra.PlayerBets[s.id] = s.bet
		extra.ActedThisRound[s.id] = s.acted
		extra.Folded[s.id] = s.folded
		extra.PlayersAllIn[s.id] = s.allIn
	}
	state := game.NewState(&Rules{}, players, deck.StandardDeck())
	state.Extra = extra
	state.Phase = game.Playing
	return state, extra
}

// A round is over once everyone still able to bet has acted and matched the bet.
func TestBettingRoundComplete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		currentBet uint
		seats      []seat
		want       bool
	}{
		{
			name:       "acted and matched exactly",
			currentBet: 100,
			seats: []seat{
				{id: "a", chips: 900, bet: 100, acted: true},
				{id: "b", chips: 900, bet: 100, acted: true},
			},
			want: true,
		},
		{
			name:       "one chip short still owes",
			currentBet: 100,
			seats: []seat{
				{id: "a", chips: 900, bet: 100, acted: true},
				{id: "b", chips: 901, bet: 99, acted: true},
			},
			want: false,
		},
		{
			name:       "matched but never acted",
			currentBet: 0,
			seats: []seat{
				{id: "a", chips: 900, acted: true},
				{id: "b", chips: 900},
			},
			want: false,
		},
		{
			name:       "folded, all-in and broke seats owe nothing",
			currentBet: 100,
			seats: []seat{
				{id: "a", chips: 900, bet: 100, acted: true},
				{id: "b", chips: 900, folded: true},
				{id: "c", chips: 900, allIn: true},
				{id: "d", chips: 0},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			state, extra := seatedRound(tt.currentBet, tt.seats...)
			assert.Equal(t, tt.want, bettingRoundComplete(state, extra))
		})
	}
}

// nextToAct has to be able to come back around to the seat it started from: heads-up
// against an all-in opponent, that seat is the only one left who can act.
func TestNextToAct(t *testing.T) {
	t.Parallel()

	t.Run("wraps all the way back to the starting seat", func(t *testing.T) {
		t.Parallel()
		state, extra := seatedRound(0,
			seat{id: "a", chips: 900},
			seat{id: "b", chips: 900, folded: true},
		)

		assert.Equal(t, 0, nextToAct(state, extra, 0), "the only live seat is the one we started from")
	})

	t.Run("a seat that acted and matched is not asked again", func(t *testing.T) {
		t.Parallel()
		state, extra := seatedRound(100,
			seat{id: "a", chips: 900, bet: 100, acted: true},
			seat{id: "b", chips: 900, bet: 100, acted: true},
		)

		assert.Equal(t, -1, nextToAct(state, extra, 0), "nobody owes anything, so nobody is on turn")
	})

	t.Run("a seat that acted but was raised on owes again", func(t *testing.T) {
		t.Parallel()
		state, extra := seatedRound(200,
			seat{id: "a", chips: 900, bet: 200, acted: true},
			seat{id: "b", chips: 900, bet: 100, acted: true},
		)

		assert.Equal(t, 1, nextToAct(state, extra, 0))
	})

	t.Run("skips folded, all-in and broke seats", func(t *testing.T) {
		t.Parallel()
		state, extra := seatedRound(0,
			seat{id: "a", chips: 900, acted: true},
			seat{id: "b", chips: 900, folded: true},
			seat{id: "c", chips: 900, allIn: true},
			seat{id: "d", chips: 0},
			seat{id: "e", chips: 900},
		)

		assert.Equal(t, 4, nextToAct(state, extra, 0))
	})
}

// Postflop action starts left of the button and, heads-up against an all-in, can land on
// the button itself.
func TestFirstToActPostflop(t *testing.T) {
	t.Parallel()

	t.Run("comes back around to the dealer", func(t *testing.T) {
		t.Parallel()
		state, extra := seatedRound(0,
			seat{id: "a", chips: 900},
			seat{id: "b", chips: 900, folded: true},
		)
		extra.DealerIndex = 0
		state.CurrentTurn = 1

		assert.Equal(t, 0, firstToActPostflop(state, extra))
	})

	t.Run("a seat with no chips left cannot open", func(t *testing.T) {
		t.Parallel()
		state, extra := seatedRound(0,
			seat{id: "a", chips: 900},
			seat{id: "b", chips: 0},
			seat{id: "c", chips: 900},
		)
		extra.DealerIndex = 0

		assert.Equal(t, 2, firstToActPostflop(state, extra), "seat 1 is broke, so seat 2 opens")
	})

	t.Run("nobody able to act leaves the turn where it was", func(t *testing.T) {
		t.Parallel()
		state, extra := seatedRound(0,
			seat{id: "a", chips: 900, allIn: true},
			seat{id: "b", chips: 900, allIn: true},
		)
		extra.DealerIndex = 0
		state.CurrentTurn = 1

		assert.Equal(t, 1, firstToActPostflop(state, extra))
	})
}

// Betting only continues while at least two players can still put chips in.
func TestSettleAndAdvance_RunsOutTheBoardWhenBettingCannotContinue(t *testing.T) {
	t.Parallel()

	t.Run("two who can still bet stop at the flop", func(t *testing.T) {
		t.Parallel()
		state, extra := seatedRound(100,
			seat{id: "a", chips: 900, bet: 100, acted: true},
			seat{id: "b", chips: 900, bet: 100, acted: true},
			seat{id: "c", chips: 500, folded: true},
		)

		require.NoError(t, settleAndAdvance(state, extra))

		assert.Equal(t, Flop, extra.Phase, "there is still betting to do")
		assert.Len(t, extra.Table, 3)
	})

	t.Run("one who can still bet runs the board out", func(t *testing.T) {
		t.Parallel()
		state, extra := seatedRound(100,
			seat{id: "a", chips: 900, bet: 100, acted: true},
			seat{id: "b", chips: 0, bet: 100, acted: true},
		)

		require.NoError(t, settleAndAdvance(state, extra))

		assert.Equal(t, Showdown, extra.Phase, "a lone live player just sees the board")
		assert.Len(t, extra.Table, 5)
	})

	t.Run("the river always shows down", func(t *testing.T) {
		t.Parallel()
		state, extra := seatedRound(0,
			seat{id: "a", chips: 900, acted: true},
			seat{id: "b", chips: 900, acted: true},
		)
		extra.Phase = River

		require.NoError(t, settleAndAdvance(state, extra))

		assert.Equal(t, Showdown, extra.Phase)
		assert.True(t, extra.ReachedShowdown)
	})
}

// A hand cannot be won by somebody who folded, however good their cards were, and a hand
// nobody can classify still has to name a winner or the pot is stranded.
func TestShowdownWinners(t *testing.T) {
	t.Parallel()

	t.Run("a folded player never wins", func(t *testing.T) {
		t.Parallel()
		state, extra := seatedRound(0,
			seat{id: "a", chips: 900, folded: true},
			seat{id: "b", chips: 900},
		)
		scores := map[string]int{"a": 9000, "b": 10}

		winners := showdownWinners(state, extra, scores)

		require.Len(t, winners, 1)
		assert.Equal(t, "b", winners[0].ID, "the best hand at the table folded it")
	})

	t.Run("every tied player is named", func(t *testing.T) {
		t.Parallel()
		state, extra := seatedRound(0,
			seat{id: "a", chips: 900},
			seat{id: "b", chips: 900},
			seat{id: "c", chips: 900},
		)
		scores := map[string]int{"a": 500, "b": 500, "c": 10}

		winners := showdownWinners(state, extra, scores)

		require.Len(t, winners, 2, "a split pot names all co-winners")
		assert.Equal(t, "a", winners[0].ID)
		assert.Equal(t, "b", winners[1].ID)
	})

	// Fewer than five cards score zero, which is what a hand shown down before the
	// board filled out looks like. Zero still has to beat "no winner at all".
	t.Run("an unclassifiable hand still wins", func(t *testing.T) {
		t.Parallel()
		state, extra := seatedRound(0,
			seat{id: "a", chips: 900},
			seat{id: "b", chips: 900},
		)
		scores := map[string]int{"a": 0, "b": 0}

		assert.Len(t, showdownWinners(state, extra, scores), 2)
	})
}

// Dead money - chips from players who all folded - has to end up somewhere.
func TestBuildSidePots_DeadMoney(t *testing.T) {
	t.Parallel()

	t.Run("goes to the one player still in the hand", func(t *testing.T) {
		t.Parallel()
		state, extra := sidePotState(t, map[string]uint{"a": 100, "b": 100}, "a", "b")
		// A third player is in the hand without having contributed, so no pot layer
		// forms around them and the folded chips have nowhere else to go.
		state.Players = append(state.Players, &player.Player{ID: "c"})
		extra.PlayerChips["c"] = 0

		pots := buildSidePots(state, extra)

		assert.Empty(t, pots, "no eligible contributor means no pot layer")
		assert.Equal(t, uint(200), extra.PlayerChips["c"], "the dead money still gets awarded")
	})

	t.Run("nobody left in the hand strands it without crashing", func(t *testing.T) {
		t.Parallel()
		state, extra := sidePotState(t, map[string]uint{"a": 100, "b": 100}, "a", "b")

		var pots []Pot
		require.NotPanics(t, func() { pots = buildSidePots(state, extra) })

		assert.Empty(t, pots)
		assert.Zero(t, extra.PlayerChips["a"])
		assert.Zero(t, extra.PlayerChips["b"])
	})

	t.Run("splits evenly when several are still in", func(t *testing.T) {
		t.Parallel()
		state, extra := sidePotState(t, map[string]uint{"a": 101, "b": 0, "c": 0}, "a")
		extra.PlayerChips["b"], extra.PlayerChips["c"] = 0, 0

		require.NotPanics(t, func() { buildSidePots(state, extra) })

		assert.Equal(t, uint(101), extra.PlayerChips["b"]+extra.PlayerChips["c"],
			"the odd chip is not allowed to vanish")
	})
}

// awardPots decides who is paid from each layer.
func TestAwardPots(t *testing.T) {
	t.Parallel()

	t.Run("a tie splits the pot", func(t *testing.T) {
		t.Parallel()
		state, extra := sidePotState(t, map[string]uint{"a": 100, "b": 100, "c": 100})
		extra.Pots = []Pot{{Amount: 300, Eligible: []string{"a", "b", "c"}}}

		awardPots(state, extra, map[string]int{"a": 500, "b": 500, "c": 10})

		assert.Equal(t, uint(150), extra.PlayerChips["a"])
		assert.Equal(t, uint(150), extra.PlayerChips["b"])
		assert.Zero(t, extra.PlayerChips["c"], "a losing hand is paid nothing")
	})

	t.Run("the best hand takes it outright", func(t *testing.T) {
		t.Parallel()
		state, extra := sidePotState(t, map[string]uint{"a": 100, "b": 100})
		extra.Pots = []Pot{{Amount: 200, Eligible: []string{"a", "b"}}}

		awardPots(state, extra, map[string]int{"a": 10, "b": 500})

		assert.Zero(t, extra.PlayerChips["a"])
		assert.Equal(t, uint(200), extra.PlayerChips["b"])
	})

	t.Run("hands nobody can classify are still paid", func(t *testing.T) {
		t.Parallel()
		state, extra := sidePotState(t, map[string]uint{"a": 100, "b": 100})
		extra.Pots = []Pot{{Amount: 200, Eligible: []string{"a", "b"}}}

		awardPots(state, extra, map[string]int{"a": 0, "b": 0})

		assert.Equal(t, uint(200), extra.PlayerChips["a"]+extra.PlayerChips["b"],
			"a pot must never be left unawarded")
	})
}

// The match is decided by the stack a player walks away with, so chips lead the ranking;
// hand strength only separates players who finished level.
func TestRankPlayers_Order(t *testing.T) {
	t.Parallel()

	t.Run("the bigger stack finishes higher", func(t *testing.T) {
		t.Parallel()
		state, extra := tableWithChips(100, 900)

		ranked := rankPlayers(state, extra)

		require.Len(t, ranked, 2)
		assert.Equal(t, "p1", ranked[0].ID, "900 beats 100")
		assert.Equal(t, "p0", ranked[1].ID)
	})

	// Two players who finish level on chips are separated by the hand they were
	// holding, which needs a board to evaluate against.
	t.Run("level stacks are separated by the hand", func(t *testing.T) {
		t.Parallel()
		state, extra := tableWithChips(500, 500)
		state.Players[0].Cards = []deck.Card{
			{Rank: deck.Two, Suit: deck.Clubs}, {Rank: deck.Three, Suit: deck.Diamonds},
		}
		state.Players[1].Cards = []deck.Card{
			{Rank: deck.Ace, Suit: deck.Clubs}, {Rank: deck.Ace, Suit: deck.Diamonds},
		}
		extra.Table = []deck.Card{
			{Rank: deck.Ace, Suit: deck.Hearts},
			{Rank: deck.Seven, Suit: deck.Spades},
			{Rank: deck.Nine, Suit: deck.Clubs},
		}

		ranked := rankPlayers(state, extra)

		assert.Equal(t, "p1", ranked[0].ID, "trip aces on the flop outranks nothing")
	})
}

// validateRaiseTo is the only thing standing between a player and an illegal bet, so every
// limit it enforces is checked at the exact value where it starts to bite.
func TestValidateRaiseTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		currentBet uint
		minRaise   uint
		bet        uint
		chips      uint
		amount     uint
		wantErr    string
	}{
		{
			name:       "a full raise is allowed",
			currentBet: 100, minRaise: 100, chips: 1000, amount: 200,
		},
		{
			name:       "raising to exactly the current bet is not a raise",
			currentBet: 100, minRaise: 100, chips: 1000, amount: 100,
			wantErr: "above current bet",
		},
		{
			name:       "raising to exactly the minimum is allowed",
			currentBet: 100, minRaise: 50, chips: 1000, amount: 150,
		},
		{
			name:       "one chip under the minimum is refused",
			currentBet: 100, minRaise: 50, chips: 1000, amount: 149,
			wantErr: "minimum raise is 50",
		},
		{
			name:       "chips already in the pot count towards the raise",
			currentBet: 100, minRaise: 50, bet: 100, chips: 100, amount: 200,
		},
		{
			name:       "more than the stack is refused",
			currentBet: 100, minRaise: 50, chips: 100, amount: 201,
			wantErr: "not enough chips",
		},
		{
			name:       "raising the whole stack is allowed",
			currentBet: 100, minRaise: 50, chips: 200, amount: 200,
		},
		{
			name:       "an all-in below the minimum is still allowed",
			currentBet: 100, minRaise: 500, chips: 150, amount: 150,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, extra := seatedRound(tt.currentBet, seat{id: "a", chips: tt.chips, bet: tt.bet})
			extra.MinRaise = tt.minRaise

			err := validateRaiseTo(extra, &player.Player{ID: "a"}, tt.amount)

			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

// Going all-in has to move every last chip, including when the player already has more in
// front of them this street than they have left behind it.
func TestApplyAction_AllInCommitsTheWholeStack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		bet     uint
		chips   uint
		wantBet uint
	}{
		{name: "nothing committed yet", bet: 0, chips: 400, wantBet: 400},
		{name: "already bet more than is left", bet: 100, chips: 40, wantBet: 140},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			state, extra := seatedRound(0, seat{id: "a", chips: tt.chips, bet: tt.bet})
			extra.MinRaise = DefaultBigBlind
			state.CurrentTurn = 0

			(&Rules{}).ApplyAction(state, ActionAllIn{})

			assert.Zero(t, extra.PlayerChips["a"], "all-in leaves no chips behind")
			assert.Equal(t, tt.wantBet, extra.PlayerBets["a"])
			assert.True(t, extra.PlayersAllIn["a"])
		})
	}
}

// A call moves exactly the difference between what a player has in and what is owed: no
// more (that would be a raise nobody asked for) and no less.
func TestCallTo_MovesExactlyWhatIsOwed(t *testing.T) {
	t.Parallel()

	t.Run("with chips to spare", func(t *testing.T) {
		t.Parallel()
		_, extra := seatedRound(200, seat{id: "a", chips: 500, bet: 50})

		callTo(extra, &player.Player{ID: "a"}, 200)

		assert.Equal(t, uint(200), extra.PlayerBets["a"], "the bet lands on the amount owed")
		assert.Equal(t, uint(350), extra.PlayerChips["a"], "only the difference leaves the stack")
		assert.Equal(t, uint(150), extra.TotalContributed["a"])
		assert.False(t, extra.PlayersAllIn["a"])
	})

	t.Run("a short stack calls for what it has", func(t *testing.T) {
		t.Parallel()
		_, extra := seatedRound(500, seat{id: "a", chips: 100, bet: 50})

		callTo(extra, &player.Player{ID: "a"}, 500)

		assert.Equal(t, uint(150), extra.PlayerBets["a"])
		assert.Zero(t, extra.PlayerChips["a"])
		assert.True(t, extra.PlayersAllIn["a"])
	})
}

// Whether a raise is "full" decides if already-acted players get fresh action.
func TestApplyBetIncrease_Boundaries(t *testing.T) {
	t.Parallel()

	t.Run("exactly the minimum raise reopens the round", func(t *testing.T) {
		t.Parallel()
		state, extra := seatedRound(100,
			seat{id: "a", chips: 900, bet: 200},
			seat{id: "b", chips: 900, bet: 100, acted: true},
		)
		extra.MinRaise = 100

		applyBetIncrease(extra, state, state.Players[0], 200)

		assert.Equal(t, uint(200), extra.CurrentBet)
		assert.Equal(t, uint(100), extra.MinRaise, "a raise of exactly the minimum is a full raise")
		assert.False(t, extra.ActedThisRound["b"], "so it reopens the round")
	})

	t.Run("an increase to what is already owed changes nothing", func(t *testing.T) {
		t.Parallel()
		state, extra := seatedRound(100,
			seat{id: "a", chips: 900, bet: 100},
			seat{id: "b", chips: 900, bet: 100, acted: true},
		)
		// A zero MinRaise is what makes the difference observable: without the guard,
		// a raise of nothing would count as full and hand everybody fresh action.
		extra.MinRaise = 0

		applyBetIncrease(extra, state, state.Players[0], 100)

		assert.Equal(t, uint(100), extra.CurrentBet)
		assert.True(t, extra.ActedThisRound["b"], "nobody gets to act again over a non-raise")
	})
}

// A hand that cannot be dealt has to be reported.
func TestAfterAction_NextHandReportsABadDeal(t *testing.T) {
	t.Parallel()
	// Only one player still has chips, so there is no hand to deal.
	state, extra := tableWithChips(1000, 0)
	extra.HandNumber = 1

	err := (&Rules{}).AfterAction(state, ActionNextHand{})

	require.ErrorContains(t, err, "not enough funded players")
}

// After a seat is removed the cursor can point one past the end.
func TestAfterPlayerRemoved_ResetsAnOutOfRangeCursor(t *testing.T) {
	t.Parallel()
	state, _ := seatedRound(0,
		seat{id: "a", chips: 900},
		seat{id: "b", chips: 900},
	)
	state.CurrentTurn = len(state.Players)

	require.NotPanics(t, func() { (&Rules{}).AfterPlayerRemoved(state, 2) })

	assert.GreaterOrEqual(t, state.CurrentTurn, 0)
	assert.Less(t, state.CurrentTurn, len(state.Players))
}

// Seat 0 is a real answer from nextToAct, not the absence of one.
func TestAfterBettingAction_SeatZeroCanBeNext(t *testing.T) {
	t.Parallel()
	state, extra := seatedRound(100,
		seat{id: "a", chips: 900, bet: 50, acted: true},
		seat{id: "b", chips: 900, bet: 100, acted: true},
	)
	extra.Phase = Flop
	state.CurrentTurn = 1

	require.NoError(t, (&Rules{}).afterBettingAction(state, extra))

	require.NotNil(t, state.OverrideNextTurn)
	assert.Equal(t, 0, *state.OverrideNextTurn, "seat 0 still owes chips, so it is on turn")
	assert.Equal(t, Flop, extra.Phase, "the round is not over, so the street does not advance")
}

// The blinds walk forward from the button over seats that still have chips.
func TestNextFundedSeat_SkipsBrokeSeatsForward(t *testing.T) {
	t.Parallel()

	t.Run("steps over the broke seat next door", func(t *testing.T) {
		t.Parallel()
		state, extra := seatedRound(0,
			seat{id: "a", chips: 900},
			seat{id: "b", chips: 0},
			seat{id: "c", chips: 900},
		)

		assert.Equal(t, 2, nextFundedSeat(state, extra, 0), "seat 1 is out, so the blind moves to seat 2")
	})

	t.Run("nobody else funded leaves the marker where it was", func(t *testing.T) {
		t.Parallel()
		state, extra := seatedRound(0,
			seat{id: "a", chips: 900},
			seat{id: "b", chips: 0},
		)

		assert.Equal(t, 0, nextFundedSeat(state, extra, 0))
	})
}

// Seat 0 is a legitimate answer for "who is under the gun".
func TestBeginHand_SeatZeroCanBeUnderTheGun(t *testing.T) {
	t.Parallel()
	// Heads-up the button acts first, so with the button on seat 0 the first actor is
	// seat 0 itself.
	state, extra := tableWithChips(1000, 1000)

	require.NoError(t, (&Rules{}).beginHand(state, extra, 0))

	assert.Equal(t, PreFlop, extra.Phase, "the hand starts with betting, not a board")
	assert.Empty(t, extra.Table)
	assert.Equal(t, 0, state.CurrentTurn, "the button is under the gun heads-up")
}

// Being asked whether to deal the next hand is not a move under pressure, so it gets its
// own clock.
func TestRules_TurnTimeout_DealGetsALongerClock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*State)
		want   time.Duration
	}{
		{name: "a betting turn keeps the engine's clock", mutate: func(*State) {}},
		{
			name:   "between hands the dealer gets a minute",
			mutate: func(e *State) { e.HandComplete = true },
			want:   DealTurnTimeout,
		},
		{
			name:   "a finished match needs no deal clock",
			mutate: func(e *State) { e.HandComplete, e.MatchComplete = true, true },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			state, extra := seatedRound(0, seat{id: "a", chips: 900}, seat{id: "b", chips: 900})
			tt.mutate(extra)

			assert.Equal(t, tt.want, (&Rules{}).TurnTimeout(state))
		})
	}
}

// A state that is not poker's must not be read as one.
func TestRules_TurnTimeout_ForeignStateFallsBackToTheDefault(t *testing.T) {
	t.Parallel()
	state := game.NewState(&Rules{}, nil, deck.StandardDeck())

	assert.Zero(t, (&Rules{}).TurnTimeout(state))
}
