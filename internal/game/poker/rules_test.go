package poker

import (
	"testing"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/player"

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

	t.Run("bet is invalid if amount is less than current bet", func(t *testing.T) {
		state := createTestState()
		state.Extra.(*State).CurrentBet = 100
		rules := &PokerRules{}
		action := ActionBet{Amount: 50}
		err := rules.PreActionCondition(state, action)
		assert.ErrorContains(t, err, "bet amount must be at least the current bet to call")
	})

	t.Run("bet is invalid if not enough chips", func(t *testing.T) {
		state := createTestState()
		state.Extra.(*State).CurrentBet = 100
		state.Extra.(*State).PlayerChips["p1"] = 10
		rules := &PokerRules{}
		action := ActionBet{Amount: 100}
		err := rules.PreActionCondition(state, action)
		assert.ErrorContains(t, err, "not enough chips")
	})

	t.Run("bet is valid if enough chips", func(t *testing.T) {
		state := createTestState()
		state.Extra.(*State).CurrentBet = 100
		rules := &PokerRules{}
		action := ActionBet{Amount: 100}
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
		assert.Equal(t, action, extra.LastAction)
	})

	t.Run("passing does nothing but updates last action", func(t *testing.T) {
		state := createTestState()
		rules := &PokerRules{}
		action := ActionPass{}
		rules.ApplyAction(state, action)
		extra := state.Extra.(*State)
		assert.Equal(t, action, extra.LastAction)
	})

	t.Run("betting updates current bet and pool", func(t *testing.T) {
		state := createTestState()
		rules := &PokerRules{}

		action := ActionBet{Amount: 50}
		rules.ApplyAction(state, action)

		extra := state.Extra.(*State)
		assert.Equal(t, uint(50), extra.CurrentBet)
		assert.Equal(t, uint(50), extra.MainPool)
		assert.Equal(t, uint(950), extra.PlayerChips["p1"])
		assert.Equal(t, uint(50), extra.PlayerBets["p1"])
		assert.Equal(t, state.Players[0], extra.PlayerRaised)
	})
}

func TestPokerRules_CheckWinCondition(t *testing.T) {
	t.Parallel()

	t.Run("game continues if multiple players active", func(t *testing.T) {
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
}

func TestPokerRules_GetStandings(t *testing.T) {
	t.Parallel()

	t.Run("all folded except one", func(t *testing.T) {
		state := createTestState()
		extra := state.Extra.(*State)
		extra.PlayersFold = append(extra.PlayersFold, state.Players[0], state.Players[2])

		rules := &PokerRules{}
		standings := rules.GetStandings(state)
		assert.Len(t, standings, 1)
		assert.Equal(t, state.Players[1].ID, standings[0].ID)
	})

	t.Run("showdown ranks by EvaluateHand", func(t *testing.T) {
		state := createTestState()
		extra := state.Extra.(*State)

		// Board: shared five cards
		extra.Table = []*deck.Card{
			{Rank: deck.Ten, Suit: deck.Spades},
			{Rank: deck.Jack, Suit: deck.Hearts},
			{Rank: deck.Queen, Suit: deck.Diamonds},
			{Rank: deck.Two, Suit: deck.Clubs},
			{Rank: deck.Three, Suit: deck.Spades},
		}

		// p1: Ace-high (no pair)
		state.Players[0].Cards = []deck.Card{
			{Rank: deck.Ace, Suit: deck.Clubs},
			{Rank: deck.Four, Suit: deck.Hearts},
		}
		// p2: pair of Kings
		state.Players[1].Cards = []deck.Card{
			{Rank: deck.King, Suit: deck.Clubs},
			{Rank: deck.King, Suit: deck.Hearts},
		}
		// p3: broadway straight T-J-Q-K-A
		state.Players[2].Cards = []deck.Card{
			{Rank: deck.Ace, Suit: deck.Spades},
			{Rank: deck.King, Suit: deck.Diamonds},
		}

		rules := &PokerRules{}
		standings := rules.GetStandings(state)

		assert.Len(t, standings, 3)
		assert.Equal(t, "p3", standings[0].ID) // straight
		assert.Equal(t, "p2", standings[1].ID) // pair
		assert.Equal(t, "p1", standings[2].ID) // high card
	})
}
