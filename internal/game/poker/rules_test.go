package poker

import (
	"testing"

	"terminalcard/internal/deck"
	"terminalcard/internal/game"
	"terminalcard/internal/player"

	"github.com/stretchr/testify/assert"
)

func createTestState() *game.State {
	rules := &PokerRules{}
	players := []*player.Player{
		{ID: "p1", Cards: []deck.Card{{Rank: deck.Two, Suit: deck.Spades}, {Rank: deck.King, Suit: deck.Hearts}}},
		{ID: "p2", Cards: []deck.Card{{Rank: deck.Three, Suit: deck.Diamonds}, {Rank: deck.Queen, Suit: deck.Clubs}}},
		{ID: "p3", Cards: []deck.Card{{Rank: deck.Four, Suit: deck.Clubs}, {Rank: deck.Jack, Suit: deck.Spades}}},
	}
	state := game.NewState(rules, players, deck.StandardDeck())
	state.Extra = &State{
		MainPool:    0,
		CurrentBet:  0,
		SmallBlind:  5,
		BigBlind:    10,
		Phase:       PreFlop,
		PlayersFold: make([]*player.Player, 0),
		Table:       make([]*deck.Card, 0),
		PlayerChips: map[string]uint{
			"p1": 1000,
			"p2": 1000,
			"p3": 1000,
		},
		PlayerBets: map[string]uint{
			"p1": 0,
			"p2": 0,
			"p3": 0,
		},
	}
	state.CurrentTurn = 0
	state.Phase = game.Playing
	return state
}

func TestPokerRules_Metadata(t *testing.T) {
	rules := &PokerRules{}
	assert.Equal(t, "Poker", rules.Name())
	assert.Equal(t, 2, rules.MinPlayers())
	assert.Equal(t, 9, rules.MaxPlayers())
	assert.Equal(t, 2, rules.InitialDealCount())
}

func TestPokerRules_PreActionCondition(t *testing.T) {
	t.Parallel()

	t.Run("fold is always valid", func(t *testing.T) {
		state := createTestState()
		rules := &PokerRules{}
		action := ActionFold{}
		err := rules.PreActionCondition(state, action)
		assert.NoError(t, err)
	})

	t.Run("pass (check) is valid when no one has bet in current round", func(t *testing.T) {
		state := createTestState()
		state.Extra.(*State).CurrentBet = 0
		rules := &PokerRules{}
		action := ActionPass{}
		err := rules.PreActionCondition(state, action)
		assert.NoError(t, err)
	})

	t.Run("pass (check) is invalid when there is a pending bet to call", func(t *testing.T) {
		state := createTestState()
		state.Extra.(*State).CurrentBet = 100
		rules := &PokerRules{}
		action := ActionPass{}
		err := rules.PreActionCondition(state, action)
		assert.ErrorContains(t, err, "cannot check, must call or raise")
	})

	t.Run("bet/raise is valid if player has enough chips", func(t *testing.T) {
		state := createTestState()
		rules := &PokerRules{}
		action := ActionBet{}
		err := rules.PreActionCondition(state, action)
		assert.NoError(t, err)
	})
}

func TestPokerRules_ApplyAction(t *testing.T) {
	t.Parallel()

	t.Run("folding adds player to folded list", func(t *testing.T) {
		state := createTestState()
		rules := &PokerRules{}
		action := ActionFold{}

		playerFolded := state.Players[state.CurrentTurn]
		rules.ApplyAction(state, action)

		extra := state.Extra.(*State)
		assert.Contains(t, extra.PlayersFold, playerFolded)
	})

	t.Run("betting updates current bet and pool", func(t *testing.T) {
		state := createTestState()
		rules := &PokerRules{}

		action := ActionBet{Amount: 50}
		rules.ApplyAction(state, action)

		extra := state.Extra.(*State)
		assert.Equal(t, uint(50), extra.CurrentBet)
		assert.Equal(t, uint(50), extra.MainPool)
	})
}

func TestPokerRules_RoundProgression(t *testing.T) {
	t.Parallel()

	t.Run("pre-flop to flop deals 3 cards", func(t *testing.T) {
		state := createTestState()
		extra := state.Extra.(*State)
		assert.Len(t, extra.Table, 0)

		// TODO: Trigger round end condition here
		// rules := &PokerRules{}
		// rules.advanceRound(state)

		// assert.Len(t, extra.Table, 3)
	})

	t.Run("flop to turn deals 1 additional card", func(t *testing.T) {
		// state := createTestState()
		// extra := state.Extra.(*State)
		//  TODO: Table with 3 cards

		// rules.advanceRound(state)

		// assert.Len(t, extra.Table, 4)
	})

	t.Run("turn to river deals 1 additional card", func(t *testing.T) {
		// state := createTestState()
		// extra := state.Extra.(*State)
		// TODO: Table with 4 cards

		// rules.advanceRound(state)

		// assert.Len(t, extra.Table, 5)
	})
}

func TestPokerRules_CheckWinCondition(t *testing.T) {
	t.Parallel()

	t.Run("game continues if multiple players active and not showdown", func(t *testing.T) {
		state := createTestState()
		rules := &PokerRules{}
		assert.False(t, rules.CheckWinCondition(state))
	})

	t.Run("player wins immediately if everyone else folded", func(t *testing.T) {
		state := createTestState()
		extra := state.Extra.(*State)

		extra.PlayersFold = append(extra.PlayersFold, state.Players[1], state.Players[2])

		rules := &PokerRules{}
		assert.True(t, rules.CheckWinCondition(state))
	})

	t.Run("showdown evaluates best hand", func(t *testing.T) {
		// 5 cards on table, 2 players remaining.
		// Player 1 has a pair, Player 2 has a flush.
		// Player 2 should be marked as the Winner.
	})
}

func TestPokerHandEvaluation(t *testing.T) {
	t.Parallel()

	t.Run("pair beats high card", func(t *testing.T) {
		// TODO: Implement EvaluateHand([]deck.Card) HandRank
	})

	t.Run("two pair beats pair", func(t *testing.T) {
	})

	t.Run("three of a kind beats two pair", func(t *testing.T) {
	})

	t.Run("straight beats three of a kind", func(t *testing.T) {
	})

	t.Run("flush beats straight", func(t *testing.T) {
	})

	t.Run("full house beats flush", func(t *testing.T) {
	})

	t.Run("four of a kind beats full house", func(t *testing.T) {
	})

	t.Run("straight flush beats four of a kind", func(t *testing.T) {
	})

	t.Run("royal flush beats straight flush", func(t *testing.T) {
	})
}
