package poker

import (
	"fmt"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/player"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startHeadsUp(t *testing.T) (*game.Engine, *Rules) {
	t.Helper()
	rules := &Rules{}
	players := []*player.Player{
		{ID: "p1"},
		{ID: "p2"},
	}
	engine := game.NewEngine(rules, players, deck.StandardDeck())
	require.NoError(t, engine.Start())
	return engine, rules
}

func extraOf(t *testing.T, e *game.Engine) *State {
	t.Helper()
	var extra *State
	e.WithState(func(s *game.State) {
		var ok bool
		extra, ok = s.Extra.(*State)
		require.True(t, ok)
	})
	return extra
}

func TestOnGameStart_HeadsUpBlinds(t *testing.T) {
	t.Parallel()
	engine, _ := startHeadsUp(t)
	extra := extraOf(t, engine)

	assert.Equal(t, PreFlop, extra.Phase)
	assert.Equal(t, DefaultSmallBlind+DefaultBigBlind, extra.MainPool)
	assert.Equal(t, DefaultBigBlind, extra.CurrentBet)

	sbChips := extra.PlayerChips[engine.CurrentPlayerID()]
	assert.Equal(t, DefaultStack-DefaultSmallBlind, sbChips)
}

func TestBetting_FoldWinsHand(t *testing.T) {
	t.Parallel()
	engine, _ := startHeadsUp(t)
	actor := engine.CurrentPlayerID()
	other := "p1"
	if actor == "p1" {
		other = "p2"
	}

	require.NoError(t, engine.SubmitAction(actor, ActionFold{}))
	assert.True(t, engine.IsFinished())
	extra := extraOf(t, engine)
	assert.Equal(t, uint(0), extra.MainPool)
	assert.Greater(t, extra.PlayerChips[other], DefaultStack)
}

func TestBetting_CheckThroughFlop(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	players := []*player.Player{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	engine := game.NewEngine(rules, players, deck.StandardDeck())
	require.NoError(t, engine.Start())

	for i := 0; i < 20 && !engine.IsFinished(); i++ {
		extra := extraOf(t, engine)
		if extra.Phase != PreFlop {
			break
		}
		id := engine.CurrentPlayerID()
		toCall := ToCall(extra, id)
		var err error
		if toCall == 0 {
			err = engine.SubmitAction(id, ActionCheck{})
		} else {
			err = engine.SubmitAction(id, ActionCall{})
		}
		require.NoError(t, err, "action %d by %s", i, id)
	}

	extra := extraOf(t, engine)
	assert.Equal(t, Flop, extra.Phase)
	assert.Len(t, extra.Table, 3)
}

func otherThan(current string) string {
	for _, id := range []string{"a", "b", "c"} {
		if id != current {
			return id
		}
	}
	return ""
}

func actCurrent(t *testing.T, engine *game.Engine) {
	t.Helper()
	extra := extraOf(t, engine)
	id := engine.CurrentPlayerID()
	if ToCall(extra, id) == 0 {
		require.NoError(t, engine.SubmitAction(id, ActionCheck{}))
	} else {
		require.NoError(t, engine.SubmitAction(id, ActionCall{}))
	}
}

func TestLeave_NonCurrentPlayerKeepsTurn(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	players := []*player.Player{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	engine := game.NewEngine(rules, players, deck.StandardDeck())
	require.NoError(t, engine.Start())

	current := engine.CurrentPlayerID()
	engine.RemovePlayer(otherThan(current))

	require.False(t, engine.IsFinished(), "two players remain")
	assert.Equal(t, current, engine.CurrentPlayerID(), "the non-leaver's turn must not be stolen by a stale index")

	extra := extraOf(t, engine)
	assert.GreaterOrEqual(t, extra.DealerIndex, 0)
	assert.Less(t, extra.DealerIndex, 2)
	actCurrent(t, engine) // the seat on turn must be able to legally act
}

func TestLeave_CurrentPlayerPassesTurnToValidActor(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	players := []*player.Player{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	engine := game.NewEngine(rules, players, deck.StandardDeck())
	require.NoError(t, engine.Start())

	current := engine.CurrentPlayerID()
	engine.RemovePlayer(current)

	require.False(t, engine.IsFinished(), "two players remain")
	next := engine.CurrentPlayerID()
	assert.NotEqual(t, current, next)
	assert.Contains(t, []string{"a", "b", "c"}, next)
	actCurrent(t, engine)
}

func TestBetting_AllInShortStackSidePotAward(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	players := []*player.Player{
		{ID: "short", Cards: []deck.Card{{Rank: deck.Ace, Suit: deck.Spades}, {Rank: deck.Ace, Suit: deck.Hearts}}},
		{ID: "mid", Cards: []deck.Card{{Rank: deck.King, Suit: deck.Spades}, {Rank: deck.King, Suit: deck.Hearts}}},
		{ID: "big", Cards: []deck.Card{{Rank: deck.Two, Suit: deck.Clubs}, {Rank: deck.Three, Suit: deck.Clubs}}},
	}
	state := game.NewState(rules, players, deck.StandardDeck())
	require.NoError(t, state.Deck.Shuffle())
	state.Phase = game.Playing
	state.CurrentTurn = 0

	extra := &State{
		DealerIndex: 2,
		SBIndex:     0,
		BBIndex:     1,
		CurrentBet:  0,
		MinRaise:    50,
		SmallBlind:  25,
		BigBlind:    50,
		Phase:       River, // force showdown after settle of this "street"
		Folded:      map[string]bool{"short": false, "mid": false, "big": false},
		PlayersAllIn: map[string]bool{
			"short": false, "mid": false, "big": false,
		},
		Table: []deck.Card{
			{Rank: deck.Ace, Suit: deck.Diamonds},
			{Rank: deck.King, Suit: deck.Diamonds},
			{Rank: deck.Seven, Suit: deck.Spades},
			{Rank: deck.Four, Suit: deck.Hearts},
			{Rank: deck.Nine, Suit: deck.Clubs},
		},
		PlayerChips: map[string]uint{
			"short": 100,
			"mid":   500,
			"big":   500,
		},
		PlayerBets:       map[string]uint{"short": 0, "mid": 0, "big": 0},
		TotalContributed: map[string]uint{"short": 0, "mid": 0, "big": 0},
		ActedThisRound:   map[string]bool{"short": false, "mid": false, "big": false},
	}
	state.Extra = extra

	// short all-in 100; mid and big put 300 each → main 300 + side 400.
	state.CurrentTurn = 0
	rules.ApplyAction(state, ActionAllIn{})
	require.NoError(t, rules.PostActionCondition(state, ActionAllIn{}))

	state.CurrentTurn = *state.OverrideNextTurn
	require.NoError(t, rules.PreActionCondition(state, ActionRaiseTo{Amount: 300}))
	rules.ApplyAction(state, ActionRaiseTo{Amount: 300})
	require.NoError(t, rules.PostActionCondition(state, ActionRaiseTo{Amount: 300}))

	state.CurrentTurn = *state.OverrideNextTurn
	require.NoError(t, rules.PreActionCondition(state, ActionCall{}))
	rules.ApplyAction(state, ActionCall{})
	require.NoError(t, rules.PostActionCondition(state, ActionCall{}))

	assert.True(t, extra.HandComplete)
	assert.Equal(t, uint(0), extra.MainPool)
	// short has trips aces → wins 300 main; mid has kings → wins 400 side vs big.
	assert.Equal(t, uint(300), extra.PlayerChips["short"])
	assert.Equal(t, uint(600), extra.PlayerChips["mid"]) // 500-300+400
	assert.Equal(t, uint(200), extra.PlayerChips["big"]) // 500-300
	assert.Equal(t, uint(1100), extra.PlayerChips["short"]+extra.PlayerChips["mid"]+extra.PlayerChips["big"])
}

func TestBuildSidePots_LayeredWithDeadMoney(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	players := []*player.Player{{ID: "p1"}, {ID: "p2"}, {ID: "p3"}, {ID: "p4"}}
	state := game.NewState(rules, players, deck.StandardDeck())
	extra := &State{
		Folded:       map[string]bool{"p1": false, "p2": false, "p3": false, "p4": true},
		PlayersAllIn: map[string]bool{"p1": false, "p2": false, "p3": false, "p4": false},
		PlayerChips:  map[string]uint{"p1": 0, "p2": 0, "p3": 0, "p4": 0},
		// p1/p2/p3 contribute distinct amounts; folded p4 leaves 50 of dead money.
		TotalContributed: map[string]uint{"p1": 100, "p2": 200, "p3": 300, "p4": 50},
	}
	state.Extra = extra

	pots := buildSidePots(state, extra)

	require.Len(t, pots, 4)
	// Layer 0-50: everyone in (incl. p4's dead money), eligible = live players.
	assert.Equal(t, Pot{Amount: 200, Eligible: []string{"p1", "p2", "p3"}}, pots[0])
	// Layer 50-100: p1/p2/p3.
	assert.Equal(t, Pot{Amount: 150, Eligible: []string{"p1", "p2", "p3"}}, pots[1])
	// Layer 100-200: p2/p3.
	assert.Equal(t, Pot{Amount: 200, Eligible: []string{"p2", "p3"}}, pots[2])
	// Layer 200-300: p3 alone.
	assert.Equal(t, Pot{Amount: 100, Eligible: []string{"p3"}}, pots[3])

	var potTotal, contribTotal uint
	for _, p := range pots {
		potTotal += p.Amount
	}
	for _, c := range extra.TotalContributed {
		contribTotal += c
	}
	assert.Equal(t, contribTotal, potTotal, "chips conserved across pots")
}

func TestBuildSidePots_UncalledOvershoveReturned(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	players := []*player.Player{{ID: "p1"}, {ID: "p2"}}
	state := game.NewState(rules, players, deck.StandardDeck())
	extra := &State{
		Folded:           map[string]bool{"p1": false, "p2": false},
		PlayersAllIn:     map[string]bool{"p1": false, "p2": false},
		PlayerChips:      map[string]uint{"p1": 0, "p2": 0},
		TotalContributed: map[string]uint{"p1": 300, "p2": 100},
	}
	state.Extra = extra

	pots := buildSidePots(state, extra)

	require.Len(t, pots, 2)
	// Contested layer up to the call amount.
	assert.Equal(t, Pot{Amount: 200, Eligible: []string{"p1", "p2"}}, pots[0])
	// Uncalled excess returns to the lone over-shover as a single-eligible pot.
	assert.Equal(t, Pot{Amount: 200, Eligible: []string{"p1"}}, pots[1])
}

func TestNextToAct_SkipsFolded(t *testing.T) {
	t.Parallel()
	state := createTestState()
	extra := state.Extra.(*State)
	extra.Folded["p2"] = true
	extra.ActedThisRound["p1"] = true
	extra.CurrentBet = 0
	next := nextToAct(state, extra, 0)
	assert.Equal(t, 2, next)
}

func TestLeave_FoldsAndAwardsPot(t *testing.T) {
	t.Parallel()
	engine, _ := startHeadsUp(t)
	leaver := engine.CurrentPlayerID()
	other := "p1"
	if leaver == "p1" {
		other = "p2"
	}

	engine.RemovePlayer(leaver)
	assert.True(t, engine.IsFinished())
	extra := extraOf(t, engine)
	assert.True(t, extra.HandComplete)
	assert.Equal(t, uint(0), extra.MainPool)
	assert.Greater(t, extra.PlayerChips[other], DefaultStack)
}

func TestLeave_MultiwayContinues(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	players := []*player.Player{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	engine := game.NewEngine(rules, players, deck.StandardDeck())
	require.NoError(t, engine.Start())

	engine.RemovePlayer("b")
	assert.False(t, engine.IsFinished())
	extra := extraOf(t, engine)
	assert.True(t, extra.Folded["b"])
	assert.Equal(t, 2, func() int {
		var n int
		engine.WithState(func(s *game.State) { n = len(s.Players) })
		return n
	}())
}

func TestRaiseTo_UsesCommittedBet(t *testing.T) {
	t.Parallel()
	state := createTestState()
	extra := state.Extra.(*State)
	extra.PlayerChips["p1"] = 40
	extra.CurrentBet = 0
	// Apply without PreAction — commit clamps; CurrentBet must follow committed amount.
	(&Rules{}).ApplyAction(state, ActionRaiseTo{Amount: 100})
	assert.Equal(t, uint(40), extra.CurrentBet)
	assert.Equal(t, uint(40), extra.PlayerBets["p1"])
	assert.Equal(t, uint(0), extra.PlayerChips["p1"])
}

func TestStandings_ChopStableByID(t *testing.T) {
	t.Parallel()
	state := createTestState()
	extra := state.Extra.(*State)
	extra.Table = []deck.Card{
		{Rank: deck.Ace, Suit: deck.Spades},
		{Rank: deck.King, Suit: deck.Spades},
		{Rank: deck.Queen, Suit: deck.Spades},
		{Rank: deck.Jack, Suit: deck.Spades},
		{Rank: deck.Nine, Suit: deck.Hearts},
	}
	// Identical hands (board plays) → stable ID order.
	state.Players[0].Cards = []deck.Card{{Rank: deck.Two, Suit: deck.Clubs}, {Rank: deck.Three, Suit: deck.Clubs}}
	state.Players[1].Cards = []deck.Card{{Rank: deck.Four, Suit: deck.Clubs}, {Rank: deck.Five, Suit: deck.Clubs}}
	state.Players[2].Cards = []deck.Card{{Rank: deck.Six, Suit: deck.Clubs}, {Rank: deck.Seven, Suit: deck.Clubs}}
	extra.PlayerChips["p1"] = 1000
	extra.PlayerChips["p2"] = 1000
	extra.PlayerChips["p3"] = 1000

	standings := (&Rules{}).GetStandings(state)
	assert.Equal(t, []string{"p1", "p2", "p3"}, []string{standings[0].ID, standings[1].ID, standings[2].ID})
}

func TestSmoke_PassiveHands(t *testing.T) {
	t.Parallel()
	for _, n := range []int{2, 6} {
		t.Run(fmt.Sprintf("%d-max", n), func(t *testing.T) {
			t.Parallel()
			rules := &Rules{}
			players := make([]*player.Player, n)
			for i := range players {
				players[i] = &player.Player{ID: fmt.Sprintf("p%d", i)}
			}
			engine := game.NewEngine(rules, players, deck.StandardDeck())
			require.NoError(t, engine.Start())
			for i := 0; i < 100 && !engine.IsFinished(); i++ {
				extra := extraOf(t, engine)
				id := engine.CurrentPlayerID()
				toCall := ToCall(extra, id)
				var err error
				if toCall == 0 {
					err = engine.SubmitAction(id, ActionCheck{})
				} else {
					err = engine.SubmitAction(id, ActionCall{})
				}
				require.NoError(t, err)
			}
			assert.True(t, engine.IsFinished())
			extra := extraOf(t, engine)
			assert.Len(t, extra.Table, 5)
			var total uint
			for _, p := range players {
				total += extra.PlayerChips[p.ID]
			}
			// Departed none; chip conservation.
			assert.Equal(t, DefaultStack*uint(n), total)
		})
	}
}
