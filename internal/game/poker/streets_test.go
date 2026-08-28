package poker

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// testingT is the slice of *testing.T and *rapid.T these helpers need. Threading
// rapid's own T into them is what lets a property failure be reported to rapid,
// which then shrinks the counterexample; reporting to the parent *testing.T instead
// aborts the property goroutine and loses the seed.
type testingT interface {
	require.TestingT
	Helper()
}

func startHeadsUp(t testingT) *game.Engine {
	t.Helper()
	return startTable(t, 2)
}

func extraOf(t testingT, e *game.Engine) *State {
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
	engine := startHeadsUp(t)
	extra := extraOf(t, engine)

	assert.Equal(t, PreFlop, extra.Phase)
	assert.Equal(t, DefaultSmallBlind+DefaultBigBlind, extra.MainPool)
	assert.Equal(t, DefaultBigBlind, extra.CurrentBet)

	sbChips := extra.PlayerChips[engine.CurrentPlayerID()]
	assert.Equal(t, DefaultStack-DefaultSmallBlind, sbChips)
}

func TestBetting_FoldWinsHand(t *testing.T) {
	t.Parallel()
	engine := startHeadsUp(t)
	actor := engine.CurrentPlayerID()
	other := "p1"
	if actor == "p1" {
		other = "p2"
	}

	require.NoError(t, engine.SubmitAction(actor, ActionFold{}))
	extra := extraOf(t, engine)
	assert.True(t, extra.HandComplete)
	assert.False(t, engine.IsFinished(), "one hand does not end a match")
	assert.Equal(t, uint(0), extra.MainPool)
	assert.Greater(t, extra.PlayerChips[other], DefaultStack)

	// The pot is settled, so the only thing left to do is deal the next hand.
	dealer := engine.CurrentPlayerID()
	require.NoError(t, engine.SubmitAction(dealer, ActionNextHand{}))
	extra = extraOf(t, engine)
	assert.False(t, extra.HandComplete)
	assert.Equal(t, 2, extra.HandNumber)
	assert.Equal(t, PreFlop, extra.Phase)
}

func TestBetting_CheckThroughFlop(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	players := []*game.Player{{ID: "a"}, {ID: "b"}, {ID: "c"}}
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
	players := []*game.Player{{ID: "a"}, {ID: "b"}, {ID: "c"}}
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
	players := []*game.Player{{ID: "a"}, {ID: "b"}, {ID: "c"}}
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
	players := []*game.Player{
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

	// short all-in 100; mid and big put 300 each -> main 300 + side 400.
	state.CurrentTurn = 0
	rules.ApplyAction(state, ActionAllIn{})
	require.NoError(t, rules.AfterAction(state, ActionAllIn{}))

	state.CurrentTurn = *state.OverrideNextTurn
	require.NoError(t, rules.ValidateAction(state, ActionRaiseTo{Amount: 300}))
	rules.ApplyAction(state, ActionRaiseTo{Amount: 300})
	require.NoError(t, rules.AfterAction(state, ActionRaiseTo{Amount: 300}))

	state.CurrentTurn = *state.OverrideNextTurn
	require.NoError(t, rules.ValidateAction(state, ActionCall{}))
	rules.ApplyAction(state, ActionCall{})
	require.NoError(t, rules.AfterAction(state, ActionCall{}))

	assert.True(t, extra.HandComplete)
	assert.Equal(t, uint(0), extra.MainPool)
	// short has trips aces -> wins 300 main; mid has kings -> wins 400 side vs big.
	assert.Equal(t, uint(300), extra.PlayerChips["short"])
	assert.Equal(t, uint(600), extra.PlayerChips["mid"]) // 500-300+400
	assert.Equal(t, uint(200), extra.PlayerChips["big"]) // 500-300
	assert.Equal(t, uint(1100), extra.PlayerChips["short"]+extra.PlayerChips["mid"]+extra.PlayerChips["big"])
}

func TestBuildSidePots_LayeredWithDeadMoney(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	players := []*game.Player{{ID: "p1"}, {ID: "p2"}, {ID: "p3"}, {ID: "p4"}}
	state := game.NewState(rules, players, deck.StandardDeck())
	extra := &State{
		Folded:       map[string]bool{"p1": false, "p2": false, "p3": false, "p4": true},
		PlayersAllIn: map[string]bool{"p1": false, "p2": false, "p3": false, "p4": false},
		PlayerChips:  map[string]uint{"p1": 0, "p2": 0, "p3": 0, "p4": 0},
		// p1/p2/p3 contribute distinct amounts; folded p4 leaves 50 of dead money.
		TotalContributed: map[string]uint{"p1": 100, "p2": 200, "p3": 300, "p4": 50},
	}
	state.Extra = extra

	pots := buildSidePots(state, extra, contenders(state, extra))

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
	players := []*game.Player{{ID: "p1"}, {ID: "p2"}}
	state := game.NewState(rules, players, deck.StandardDeck())
	extra := &State{
		Folded:           map[string]bool{"p1": false, "p2": false},
		PlayersAllIn:     map[string]bool{"p1": false, "p2": false},
		PlayerChips:      map[string]uint{"p1": 0, "p2": 0},
		TotalContributed: map[string]uint{"p1": 300, "p2": 100},
	}
	state.Extra = extra

	pots := buildSidePots(state, extra, contenders(state, extra))

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
	engine := startHeadsUp(t)
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
	players := []*game.Player{{ID: "a"}, {ID: "b"}, {ID: "c"}}
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
	// Apply without PreAction - commit clamps; CurrentBet must follow committed amount.
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
	// Identical hands (board plays) -> stable ID order.
	state.Players[0].Cards = []deck.Card{{Rank: deck.Two, Suit: deck.Clubs}, {Rank: deck.Three, Suit: deck.Clubs}}
	state.Players[1].Cards = []deck.Card{{Rank: deck.Four, Suit: deck.Clubs}, {Rank: deck.Five, Suit: deck.Clubs}}
	state.Players[2].Cards = []deck.Card{{Rank: deck.Six, Suit: deck.Clubs}, {Rank: deck.Seven, Suit: deck.Clubs}}
	extra.PlayerChips["p1"] = 1000
	extra.PlayerChips["p2"] = 1000
	extra.PlayerChips["p3"] = 1000

	standings := (&Rules{}).Standings(state)
	assert.Equal(t, []string{"p1", "p2", "p3"}, []string{standings[0].ID, standings[1].ID, standings[2].ID})
}

func TestSmoke_PassiveHands(t *testing.T) {
	t.Parallel()
	for _, n := range []int{2, 6} {
		t.Run(fmt.Sprintf("%d-max", n), func(t *testing.T) {
			t.Parallel()
			rules := &Rules{}
			players := make([]*game.Player, n)
			for i := range players {
				players[i] = &game.Player{ID: fmt.Sprintf("p%d", i)}
			}
			engine := game.NewEngine(rules, players, deck.StandardDeck())
			require.NoError(t, engine.Start())
			// Every hand of a passive match: check it down, then deal the next one.
			for i := 0; i < 100*HandsPerMatch && !engine.IsFinished(); i++ {
				extra := extraOf(t, engine)
				id := engine.CurrentPlayerID()
				var err error
				switch {
				case extra.HandComplete:
					err = engine.SubmitAction(id, ActionNextHand{})
				case ToCall(extra, id) == 0:
					err = engine.SubmitAction(id, ActionCheck{})
				default:
					err = engine.SubmitAction(id, ActionCall{})
				}
				require.NoError(t, err)
			}
			assert.True(t, engine.IsFinished())
			extra := extraOf(t, engine)
			assert.Equal(t, HandsPerMatch, extra.HandNumber, "a passive match runs the full distance")
			assert.Len(t, extra.Table, 5)
			var total uint
			for _, p := range players {
				total += extra.PlayerChips[p.ID]
			}
			// Departed none; chip conservation across every hand of the match.
			assert.Equal(t, DefaultStack*uint(n), total)
		})
	}
}

func startTable(t testingT, n int) *game.Engine {
	t.Helper()
	players := make([]*game.Player, 0, n)
	for i := range n {
		players = append(players, &game.Player{ID: fmt.Sprintf("p%d", i+1)})
	}
	engine := game.NewEngine(&Rules{}, players, deck.StandardDeck())
	require.NoError(t, engine.Start())
	return engine
}

// candidateActions is every action worth attempting, cheapest first. Illegal ones
// are rejected by the engine, so the driver can just try the next. ActionNextHand is not in
// here: only the parked dealer may submit it, so the driver handles it separately.
func candidateActions(raiseTo uint) []game.Action {
	return []game.Action{
		ActionCheck{},
		ActionCall{},
		ActionRaiseTo{Amount: raiseTo},
		ActionAllIn{},
		ActionFold{},
	}
}

func TestChipsAreConservedAcrossRandomHands(t *testing.T) {
	t.Parallel()

	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(2, 6).Draw(rt, "players")
		engine := startTable(rt, n)
		defer engine.Close()
		want := uint(n) * DefaultStack

		require.Equal(rt, want, chipsInPlay(extraOf(rt, engine)), "blinds must not create or destroy chips")

		for step := range 400 {
			if engine.IsFinished() {
				break
			}
			id := engine.CurrentPlayerID()
			extra := extraOf(rt, engine)

			if extra.HandComplete {
				// The pot is settled between hands, so every chip is in a stack.
				require.Zero(rt, extra.MainPool, "a completed hand must leave nothing in the pool")
				require.Equal(rt, want, chipsInPlay(extra), "payouts must return exactly what was collected")
				// Only the parked dealer may deal, and it is their turn.
				require.NoError(rt, engine.SubmitAction(id, ActionNextHand{}), "dealing the next hand")
				require.Equal(rt, want, chipsInPlay(extraOf(rt, engine)), "the next hand's blinds must not mint chips")
				continue
			}

			// Bias toward a legal raise size so the raise path is actually exercised.
			raiseTo := extra.CurrentBet + extra.MinRaise +
				uint(rapid.IntRange(0, 200).Draw(rt, fmt.Sprintf("raise%d", step)))
			order := rapid.IntRange(0, 4).Draw(rt, fmt.Sprintf("pick%d", step))

			actions := candidateActions(raiseTo)
			acted := false
			for i := range actions {
				act := actions[(order+i)%len(actions)]
				if err := engine.SubmitAction(id, act); err == nil {
					acted = true
					break
				}
			}
			if !acted {
				break // no legal action for the player on turn
			}

			require.Equal(rt, want, chipsInPlay(extraOf(rt, engine)),
				"chips changed after step %d (%d players)", step, n)
		}

		final := extraOf(rt, engine)
		if final.HandComplete {
			require.Zero(rt, final.MainPool, "a completed hand must leave nothing in the pool")
			require.Equal(rt, want, chipsInPlay(final), "payouts must return exactly the chips collected")
		}
	})
}

// Everyone all-in before the flop: no betting can continue, so the board has to run
// out to the river and pay out. runOutBoard had no direct coverage.
func TestAllInPreflop_RunsOutBoardAndPays(t *testing.T) {
	t.Parallel()
	engine := startTable(t, 3)
	t.Cleanup(engine.Close)
	want := 3 * DefaultStack

	for range 10 {
		if engine.IsFinished() || extraOf(t, engine).HandComplete {
			break
		}
		require.NoError(t, engine.SubmitAction(engine.CurrentPlayerID(), ActionAllIn{}))
	}

	extra := extraOf(t, engine)
	require.True(t, extra.HandComplete, "all-in table must resolve without further action")
	assert.Equal(t, Showdown, extra.Phase)
	assert.Len(t, extra.Table, 5, "board runs out to the river when nobody can act")
	assert.NotEmpty(t, extra.Winners)

	var stacks uint
	for _, c := range extra.PlayerChips {
		stacks += c
	}
	assert.Equal(t, want, stacks)
	assert.Zero(t, extra.MainPool)
}

// Three unequal stacks all-in build a main pot plus side pots; the short stack can
// only win the layer it paid for.
func TestUnequalAllIns_ShortStackCannotWinSidePot(t *testing.T) {
	t.Parallel()
	engine := startTable(t, 3)
	t.Cleanup(engine.Close)

	engine.WithState(func(s *game.State) {
		extra, ok := s.Extra.(*State)
		require.True(t, ok)
		extra.PlayerChips[s.Players[0].ID] = 100
		extra.PlayerChips[s.Players[1].ID] = 500
		extra.PlayerChips[s.Players[2].ID] = 900
		// Rewriting stacks mid-hand moves the conservation baseline with them.
		extra.handStartChips = chipsInPlay(extra)
	})
	before := chipsInPlay(extraOf(t, engine))

	for range 10 {
		if engine.IsFinished() || extraOf(t, engine).HandComplete {
			break
		}
		require.NoError(t, engine.SubmitAction(engine.CurrentPlayerID(), ActionAllIn{}))
	}

	extra := extraOf(t, engine)
	require.True(t, extra.HandComplete)
	assert.Equal(t, before, chipsInPlay(extra), "side-pot split must conserve chips")

	for _, pot := range extra.Pots {
		assert.NotEmpty(t, pot.Eligible, "a pot with no eligible player would strand chips")
	}
}

// sidePotState builds just enough State for buildSidePots/awardPots. Options keep
// each case's intent visible instead of restating a full literal every time.
func sidePotState(t testingT, contributed map[string]uint, folded ...string) (*game.State, *State) {
	t.Helper()
	players := make([]*game.Player, 0, len(contributed))
	for id := range contributed {
		players = append(players, &game.Player{ID: id})
	}
	slices.SortFunc(players, func(a, b *game.Player) int { return cmp.Compare(a.ID, b.ID) })

	extra := &State{
		Folded:           map[string]bool{},
		PlayersAllIn:     map[string]bool{},
		PlayerChips:      map[string]uint{},
		PlayerBets:       map[string]uint{},
		TotalContributed: maps.Clone(contributed),
		ActedThisRound:   map[string]bool{},
	}
	for _, id := range folded {
		extra.Folded[id] = true
	}
	state := game.NewState(&Rules{}, players, deck.StandardDeck())
	state.Extra = extra
	return state, extra
}

func potTotal(pots []Pot) uint {
	var total uint
	for _, p := range pots {
		total += p.Amount
	}
	return total
}

// Every chip a folded player put in must still reach a pot. When the only players
// who reached the top level have folded, that layer is dead money and carries into
// the last live pot rather than vanishing.
func TestBuildSidePots_DeadMoneyCarriesIntoTheLastPot(t *testing.T) {
	t.Parallel()
	// b folded after over-committing; only a can win anything.
	state, extra := sidePotState(t, map[string]uint{"a": 50, "b": 100}, "b")

	pots := buildSidePots(state, extra, contenders(state, extra))

	require.Len(t, pots, 1, "only the level a reached can be contested")
	assert.Equal(t, []string{"a"}, pots[0].Eligible)
	assert.Equal(t, uint(150), pots[0].Amount, "b's dead 50 carries into a's pot")
	assert.Equal(t, uint(150), potTotal(pots), "no contributed chip may be lost")
}

// If every contributor folded, no pot layer forms at all and the orphaned chips must
// still be handed to the one player left standing.
func TestBuildSidePots_AllContributorsFoldedAwardsTheLoneSurvivor(t *testing.T) {
	t.Parallel()
	state, extra := sidePotState(t,
		map[string]uint{"a": 100, "b": 100, "survivor": 0}, "a", "b")

	pots := buildSidePots(state, extra, contenders(state, extra))

	assert.Empty(t, pots, "no contested layer can form")
	assert.Equal(t, uint(200), extra.PlayerChips["survivor"], "orphaned chips go to the survivor")
}

// Same shape, but with more than one survivor the orphan is split and the odd chip
// is handed out rather than dropped.
func TestBuildSidePots_OrphanSplitsAcrossSurvivorsWithoutLosingTheOddChip(t *testing.T) {
	t.Parallel()
	state, extra := sidePotState(t,
		map[string]uint{"folded": 100, "x": 0, "y": 0, "z": 0}, "folded")

	pots := buildSidePots(state, extra, contenders(state, extra))

	assert.Empty(t, pots)
	total := extra.PlayerChips["x"] + extra.PlayerChips["y"] + extra.PlayerChips["z"]
	assert.Equal(t, uint(100), total, "100 split three ways must still total 100")
}

// A contributor who left mid-hand keeps their contribution in the pot but must not
// remain eligible to win it.
func TestBuildSidePots_DepartedContributorIsNotEligible(t *testing.T) {
	t.Parallel()
	state, extra := sidePotState(t, map[string]uint{"stayed": 100, "left": 100})

	// Drop "left" from the seats, exactly as Engine.RemovePlayer does.
	state.Players = slices.DeleteFunc(state.Players, func(p *game.Player) bool { return p.ID == "left" })

	pots := buildSidePots(state, extra, contenders(state, extra))

	require.Len(t, pots, 1)
	assert.Equal(t, []string{"stayed"}, pots[0].Eligible, "a departed player cannot win")
	assert.Equal(t, uint(200), pots[0].Amount, "their chips stay in the pot")
}

// A three-way chop of a pot that does not divide evenly must distribute every chip.
func TestAwardPots_OddChipRemainderIsDistributed(t *testing.T) {
	t.Parallel()
	state, extra := sidePotState(t, map[string]uint{"a": 34, "b": 33, "c": 33})
	extra.Pots = []Pot{{Amount: 100, Eligible: []string{"a", "b", "c"}}}
	scores := map[string]int{"a": 500, "b": 500, "c": 500} // dead tie

	awardPots(extra, contenders(state, extra), scores)

	total := extra.PlayerChips["a"] + extra.PlayerChips["b"] + extra.PlayerChips["c"]
	assert.Equal(t, uint(100), total, "100 chopped three ways must still total 100")
	assert.Zero(t, extra.MainPool)
}

// The winners a showdown names must be exactly the players it paid. They were
// computed from a global best hand instead, which ignores pot eligibility: the short
// stack holding the best hand was announced as the winner of a side pot they were
// never in.
func TestRunShowdown_NamesTheWinnersItPaid(t *testing.T) {
	t.Parallel()
	// p0 shoved 100 holding the best hand; p1 and p2 put in 500 each, so the side pot
	// above 100 is contested between p1 and p2 alone.
	players := []*game.Player{
		{ID: "p0", Cards: []deck.Card{{Rank: deck.Ace, Suit: deck.Spades}, {Rank: deck.Ace, Suit: deck.Hearts}}},
		{ID: "p1", Cards: []deck.Card{{Rank: deck.King, Suit: deck.Spades}, {Rank: deck.King, Suit: deck.Hearts}}},
		{ID: "p2", Cards: []deck.Card{{Rank: deck.Two, Suit: deck.Clubs}, {Rank: deck.Three, Suit: deck.Clubs}}},
	}
	state := game.NewState(&Rules{}, players, deck.StandardDeck())
	extra := &State{
		Phase:        River,
		Folded:       map[string]bool{},
		PlayersAllIn: map[string]bool{"p0": true, "p1": true, "p2": true},
		Table: []deck.Card{
			{Rank: deck.Ace, Suit: deck.Diamonds},
			{Rank: deck.King, Suit: deck.Diamonds},
			{Rank: deck.Seven, Suit: deck.Spades},
			{Rank: deck.Four, Suit: deck.Hearts},
			{Rank: deck.Nine, Suit: deck.Clubs},
		},
		PlayerChips:      map[string]uint{"p0": 0, "p1": 0, "p2": 0},
		PlayerBets:       map[string]uint{},
		TotalContributed: map[string]uint{"p0": 100, "p1": 500, "p2": 500},
		ActedThisRound:   map[string]bool{},
	}
	state.Extra = extra
	before := maps.Clone(extra.PlayerChips)

	require.NoError(t, runShowdown(state, extra))

	var paid, named []string
	for id, chips := range extra.PlayerChips {
		if chips > before[id] {
			paid = append(paid, id)
		}
	}
	for _, p := range extra.Winners {
		named = append(named, p.ID)
	}
	assert.ElementsMatch(t, paid, named, "the hand must name exactly the players it paid")
	assert.Contains(t, named, "p1", "p1 took the side pot, so p1 won part of this hand")
	assert.Equal(t, uint(300), extra.PlayerChips["p0"], "the best hand only paid into the main pot")
	assert.Equal(t, uint(800), extra.PlayerChips["p1"])
}

// An all-in player has no decisions left to make, so losing their connection cannot
// cost them a pot they are already committed to. Leaving with chips behind still
// forfeits the hand.
func TestLeave_AllInPlayerStaysInThePot(t *testing.T) {
	t.Parallel()
	engine := startTable(t, 3)
	t.Cleanup(engine.Close)

	// The seat on turn gets a stack short enough that calling it leaves the other two
	// with chips behind, so the hand carries on past the shove.
	shover := engine.CurrentPlayerID()
	engine.WithState(func(s *game.State) {
		extra, ok := s.Extra.(*State)
		require.True(t, ok)
		extra.PlayerChips[shover] = 100
		extra.handStartChips = chipsInPlay(extra)
	})

	require.NoError(t, engine.SubmitAction(shover, ActionAllIn{}))
	for range 2 {
		require.NoError(t, engine.SubmitAction(engine.CurrentPlayerID(), ActionCall{}))
	}

	extra := extraOf(t, engine)
	require.True(t, extra.PlayersAllIn[shover])
	require.False(t, extra.HandComplete, "two players with chips behind keep the hand alive")

	engine.RemovePlayer(shover)
	assert.False(t, extraOf(t, engine).Folded[shover], "an all-in player has nothing left to fold")

	for range 20 {
		if engine.IsFinished() || extraOf(t, engine).HandComplete {
			break
		}
		actCurrent(t, engine)
	}

	extra = extraOf(t, engine)
	require.True(t, extra.HandComplete)
	require.NotEmpty(t, extra.Pots)
	assert.Contains(t, extra.Pots[0].Eligible, shover,
		"the all-in leaver still contests the pot they paid for")
}

// runOutBoard is entered from whichever street the last player able to bet finished
// on, not only from preflop.
func TestRunOutBoard_FillsTheBoardFromAnyStreet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		from    RoundPhase
		onBoard int
	}{
		{name: "from preflop", from: PreFlop, onBoard: 0},
		{name: "from the flop", from: Flop, onBoard: 3},
		{name: "from the turn", from: Turn, onBoard: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			state, extra := seatedRound(0,
				seat{id: "a", chips: 0, allIn: true},
				seat{id: "b", chips: 0, allIn: true},
			)
			extra.Phase = tt.from
			extra.TotalContributed = map[string]uint{"a": 500, "b": 500}
			require.NoError(t, state.Deck.Shuffle())
			for range tt.onBoard {
				c, ok := state.Deck.Draw()
				require.True(t, ok)
				extra.Table = append(extra.Table, c)
			}

			require.NoError(t, runOutBoard(state, extra))

			assert.Equal(t, Showdown, extra.Phase)
			assert.Len(t, extra.Table, 5, "the board always finishes at five cards")
			assert.True(t, extra.HandComplete)
			assert.Equal(t, uint(1000), extra.PlayerChips["a"]+extra.PlayerChips["b"],
				"every contributed chip is paid out")
			assert.Zero(t, extra.MainPool)
		})
	}
}
