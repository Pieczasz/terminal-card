package hearts

import (
	"fmt"
	"slices"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fourPlayers(hands ...[]deck.Card) []*game.Player {
	out := make([]*game.Player, playerCount)
	for i := range playerCount {
		cards := []deck.Card{}
		if i < len(hands) {
			cards = hands[i]
		}
		out[i] = &game.Player{ID: fmt.Sprintf("p%d", i+1), Cards: cards}
	}
	return out
}

func createTestState(hands ...[]deck.Card) *game.State {
	rules := &Rules{}
	players := fourPlayers(hands...)
	state := game.NewState(rules, players, nil)
	extra := &State{
		Stage:            StageTrickPlay,
		TrickCards:       make(map[string]deck.Card, playerCount),
		HandPoints:       make(map[string]int, playerCount),
		CumulativeScores: make(map[string]int, playerCount),
		TargetScore:      DefaultTargetScore,
	}
	for _, p := range players {
		extra.HandPoints[p.ID] = 0
		extra.CumulativeScores[p.ID] = 0
	}
	state.Extra = extra
	state.CurrentTurn = 0
	state.Phase = game.Playing
	return state
}

func TestRules_ValidateAction_Pass(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	hand := []deck.Card{
		{Rank: deck.Two, Suit: deck.Clubs},
		{Rank: deck.Three, Suit: deck.Clubs},
		{Rank: deck.Four, Suit: deck.Clubs},
		{Rank: deck.Five, Suit: deck.Clubs},
	}

	t.Run("accepts three owned cards", func(t *testing.T) {
		t.Parallel()
		state := createTestState(hand)
		state.Extra.(*State).Stage = StagePassing
		state.Extra.(*State).Passed = map[string]bool{}
		err := rules.ValidateAction(state, ActionPassCards{Cards: hand[:3]})
		require.NoError(t, err)
	})

	t.Run("rejects wrong count", func(t *testing.T) {
		t.Parallel()
		state := createTestState(hand)
		state.Extra.(*State).Stage = StagePassing
		state.Extra.(*State).Passed = map[string]bool{}
		err := rules.ValidateAction(state, ActionPassCards{Cards: hand[:2]})
		require.ErrorContains(t, err, "exactly 3")
	})

	t.Run("rejects duplicate", func(t *testing.T) {
		t.Parallel()
		state := createTestState(hand)
		state.Extra.(*State).Stage = StagePassing
		state.Extra.(*State).Passed = map[string]bool{}
		err := rules.ValidateAction(state, ActionPassCards{Cards: []deck.Card{hand[0], hand[0], hand[1]}})
		require.ErrorContains(t, err, "duplicate")
	})

	t.Run("rejects second pass", func(t *testing.T) {
		t.Parallel()
		state := createTestState(hand)
		extra := state.Extra.(*State)
		extra.Stage = StagePassing
		extra.Passed = map[string]bool{"p1": true}
		err := rules.ValidateAction(state, ActionPassCards{Cards: hand[:3]})
		require.ErrorContains(t, err, "already passed")
	})
}

func TestRules_AfterAction_Pass_AppliesOnFourth(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	hands := make([][]deck.Card, 4)
	for i := range 4 {
		hands[i] = []deck.Card{
			{Rank: deck.Rank(i + 1), Suit: deck.Clubs},
			{Rank: deck.Rank(i + 1), Suit: deck.Diamonds},
			{Rank: deck.Rank(i + 1), Suit: deck.Hearts},
			{Rank: deck.Rank(i + 5), Suit: deck.Spades},
		}
	}
	// Seat 0 keeps 2♣ (index 3 is not passed) so they lead after the pass.
	hands[0][3] = twoOfClubs

	state := createTestState(hands...)
	extra := state.Extra.(*State)
	extra.Stage = StagePassing
	extra.PassDirection = PassLeft
	extra.PendingPasses = make(map[string][]deck.Card, 4)
	extra.Passed = make(map[string]bool, 4)

	for seat := range 4 {
		state.CurrentTurn = seat
		pass := hands[seat][:3]
		require.NoError(t, rules.ValidateAction(state, ActionPassCards{Cards: pass}))
		rules.ApplyAction(state, ActionPassCards{Cards: pass})
		require.NoError(t, rules.AfterAction(state, ActionPassCards{Cards: pass}))
	}

	assert.Equal(t, StageTrickPlay, extra.Stage)
	assert.Nil(t, extra.PendingPasses)
	assert.Len(t, state.Players[0].Cards, 4) // kept 1, received 3
	require.NotNil(t, state.OverrideNextTurn)
	assert.Equal(t, 0, *state.OverrideNextTurn)
	assert.True(t, slices.Contains(state.Players[0].Cards, twoOfClubs))
}

func TestRules_ValidateAction_Play(t *testing.T) {
	t.Parallel()
	rules := &Rules{}

	t.Run("must lead two of clubs on trick one", func(t *testing.T) {
		t.Parallel()
		state := createTestState([]deck.Card{
			twoOfClubs,
			{Rank: deck.Ace, Suit: deck.Spades},
		})
		err := rules.ValidateAction(state, ActionPlayCard{Card: deck.Card{Rank: deck.Ace, Suit: deck.Spades}})
		require.ErrorContains(t, err, "2 of clubs")
	})

	t.Run("must follow suit", func(t *testing.T) {
		t.Parallel()
		state := createTestState([]deck.Card{
			{Rank: deck.Ace, Suit: deck.Clubs},
			{Rank: deck.King, Suit: deck.Hearts},
		})
		extra := state.Extra.(*State)
		extra.TricksPlayed = 1
		extra.LedSuit = deck.Clubs
		extra.TrickCards["p2"] = deck.Card{Rank: deck.Two, Suit: deck.Clubs}
		err := rules.ValidateAction(state, ActionPlayCard{Card: deck.Card{Rank: deck.King, Suit: deck.Hearts}})
		require.ErrorContains(t, err, "follow suit")
	})

	t.Run("hearts cannot be led before broken", func(t *testing.T) {
		t.Parallel()
		state := createTestState([]deck.Card{
			{Rank: deck.Ace, Suit: deck.Hearts},
			{Rank: deck.King, Suit: deck.Spades},
		})
		extra := state.Extra.(*State)
		extra.TricksPlayed = 1
		err := rules.ValidateAction(state, ActionPlayCard{Card: deck.Card{Rank: deck.Ace, Suit: deck.Hearts}})
		require.ErrorContains(t, err, "not been broken")
	})

	t.Run("cannot play point card on trick one", func(t *testing.T) {
		t.Parallel()
		state := createTestState([]deck.Card{
			{Rank: deck.Ace, Suit: deck.Hearts},
			{Rank: deck.King, Suit: deck.Diamonds},
		})
		extra := state.Extra.(*State)
		extra.LedSuit = deck.Clubs
		extra.TrickCards["p2"] = deck.Card{Rank: deck.Two, Suit: deck.Clubs}
		err := rules.ValidateAction(state, ActionPlayCard{Card: deck.Card{Rank: deck.Ace, Suit: deck.Hearts}})
		require.ErrorContains(t, err, "point card")
	})
}

func TestRules_ApplyAction_Play_BreaksHeartsAndTrickWinner(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	state := createTestState(
		[]deck.Card{{Rank: deck.Five, Suit: deck.Diamonds}},
		[]deck.Card{{Rank: deck.Ace, Suit: deck.Diamonds}},
		[]deck.Card{{Rank: deck.Three, Suit: deck.Hearts}},
		[]deck.Card{{Rank: deck.Two, Suit: deck.Diamonds}},
	)
	extra := state.Extra.(*State)
	extra.TricksPlayed = 1

	plays := []struct {
		seat int
		card deck.Card
	}{
		{0, deck.Card{Rank: deck.Five, Suit: deck.Diamonds}},
		{1, deck.Card{Rank: deck.Ace, Suit: deck.Diamonds}},
		{2, deck.Card{Rank: deck.Three, Suit: deck.Hearts}},
		{3, deck.Card{Rank: deck.Two, Suit: deck.Diamonds}},
	}
	for _, play := range plays {
		state.CurrentTurn = play.seat
		rules.ApplyAction(state, ActionPlayCard{Card: play.card})
		require.NoError(t, rules.AfterAction(state, ActionPlayCard{Card: play.card}))
	}

	assert.True(t, extra.HeartsBroken)
	assert.Equal(t, "p2", extra.LastTrickWinner)
	assert.Equal(t, 1, extra.HandPoints["p2"])
	require.NotNil(t, state.OverrideNextTurn)
	assert.Equal(t, 1, *state.OverrideNextTurn)
}

// The negative case for HeartsBroken: a trick with no heart in it must leave hearts
// unbroken, or leading a heart becomes legal a trick too early.
func TestRules_ApplyAction_Play_TrickWithoutHeartsLeavesThemUnbroken(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	state := createTestState(
		[]deck.Card{{Rank: deck.Five, Suit: deck.Diamonds}},
		[]deck.Card{{Rank: deck.Ace, Suit: deck.Diamonds}},
		[]deck.Card{{Rank: deck.Three, Suit: deck.Clubs}},
		[]deck.Card{{Rank: deck.Two, Suit: deck.Diamonds}},
	)
	extra := state.Extra.(*State)
	extra.TricksPlayed = 1

	for seat, card := range []deck.Card{
		{Rank: deck.Five, Suit: deck.Diamonds},
		{Rank: deck.Ace, Suit: deck.Diamonds},
		{Rank: deck.Three, Suit: deck.Clubs},
		{Rank: deck.Two, Suit: deck.Diamonds},
	} {
		state.CurrentTurn = seat
		rules.ApplyAction(state, ActionPlayCard{Card: card})
		require.NoError(t, rules.AfterAction(state, ActionPlayCard{Card: card}))
	}

	assert.False(t, extra.HeartsBroken, "no heart was played")
	assert.Zero(t, extra.HandPoints["p2"], "a heartless trick scores nothing")
}

// The led suit is fixed by the first card of the trick, not the last. A higher card
// of another suit does not win, however high it is.
func TestRules_AfterAction_Play_LedSuitIsTheFirstCardNotTheLast(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	state := createTestState(
		[]deck.Card{{Rank: deck.Nine, Suit: deck.Clubs}},
		[]deck.Card{{Rank: deck.Ace, Suit: deck.Diamonds}},
		[]deck.Card{{Rank: deck.King, Suit: deck.Clubs}},
		[]deck.Card{{Rank: deck.Two, Suit: deck.Diamonds}},
	)
	extra := state.Extra.(*State)
	extra.TricksPlayed = 1

	for seat, card := range []deck.Card{
		{Rank: deck.Nine, Suit: deck.Clubs},   // leads clubs
		{Rank: deck.Ace, Suit: deck.Diamonds}, // higher rank, wrong suit
		{Rank: deck.King, Suit: deck.Clubs},   // highest club: takes the trick
		{Rank: deck.Two, Suit: deck.Diamonds}, // last card, and its suit must not lead
	} {
		state.CurrentTurn = seat
		rules.ApplyAction(state, ActionPlayCard{Card: card})
		require.NoError(t, rules.AfterAction(state, ActionPlayCard{Card: card}))
	}

	assert.Equal(t, "p3", extra.LastTrickWinner, "the king of clubs beats the ace of diamonds")
	require.NotNil(t, state.OverrideNextTurn)
	assert.Equal(t, 2, *state.OverrideNextTurn, "the trick winner leads the next one")
}

func TestThreeLowestCards(t *testing.T) {
	t.Parallel()

	t.Run("picks the three lowest by rank", func(t *testing.T) {
		t.Parallel()
		hand := []deck.Card{
			{Rank: deck.King, Suit: deck.Spades},
			{Rank: deck.Three, Suit: deck.Hearts},
			{Rank: deck.Ace, Suit: deck.Clubs},
			{Rank: deck.Two, Suit: deck.Diamonds},
			{Rank: deck.Ten, Suit: deck.Clubs},
			{Rank: deck.Four, Suit: deck.Spades},
		}
		got := threeLowestCards(hand)
		assert.Equal(t, []deck.Card{
			{Rank: deck.Two, Suit: deck.Diamonds},
			{Rank: deck.Three, Suit: deck.Hearts},
			{Rank: deck.Four, Suit: deck.Spades},
		}, got, "ace is high in Hearts, so it is never among the lowest")
	})

	t.Run("same-rank cards break the tie on suit", func(t *testing.T) {
		t.Parallel()
		hand := []deck.Card{
			{Rank: deck.Three, Suit: deck.Hearts},
			{Rank: deck.Two, Suit: deck.Clubs},
			{Rank: deck.King, Suit: deck.Spades},
			{Rank: deck.Two, Suit: deck.Diamonds},
		}
		assert.Equal(t, []deck.Card{
			{Rank: deck.Two, Suit: deck.Diamonds},
			{Rank: deck.Two, Suit: deck.Clubs},
			{Rank: deck.Three, Suit: deck.Hearts},
		}, threeLowestCards(hand), "equal ranks order by suit, so the pass is deterministic")
	})

	t.Run("a short hand passes whatever it has", func(t *testing.T) {
		t.Parallel()
		hand := []deck.Card{{Rank: deck.King, Suit: deck.Spades}, {Rank: deck.Two, Suit: deck.Clubs}}
		assert.Equal(t, hand, threeLowestCards(hand))
	})
}

func TestRules_CheckWinCondition_AndStandings(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	state := createTestState()
	extra := state.Extra.(*State)

	assert.False(t, rules.CheckWinCondition(state))
	extra.MatchComplete = true
	assert.True(t, rules.CheckWinCondition(state))

	extra.CumulativeScores["p1"] = 40
	extra.CumulativeScores["p2"] = 10
	extra.CumulativeScores["p3"] = 10
	extra.CumulativeScores["p4"] = 80
	standings := rules.Standings(state)
	require.Len(t, standings, 4)
	assert.Equal(t, "p2", standings[0].ID)
	assert.Equal(t, "p3", standings[1].ID)
	assert.Equal(t, "p1", standings[2].ID)
	assert.Equal(t, "p4", standings[3].ID)
}

func TestRules_TimeoutAction(t *testing.T) {
	t.Parallel()
	rules := &Rules{}

	t.Run("passing returns three cards", func(t *testing.T) {
		t.Parallel()
		hand := []deck.Card{
			{Rank: deck.Ace, Suit: deck.Spades},
			{Rank: deck.Two, Suit: deck.Clubs},
			{Rank: deck.Three, Suit: deck.Diamonds},
			{Rank: deck.King, Suit: deck.Hearts},
		}
		state := createTestState(hand)
		state.Extra.(*State).Stage = StagePassing
		act := rules.TimeoutAction(state)
		pass, ok := act.(ActionPassCards)
		require.True(t, ok)
		assert.Len(t, pass.Cards, 3)
		require.NoError(t, rules.ValidateAction(state, act))
	})

	t.Run("hand over returns next hand", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		state.Extra.(*State).Stage = StageHandOver
		act := rules.TimeoutAction(state)
		assert.Equal(t, ActionNextHand{}, act)
		require.NoError(t, rules.ValidateAction(state, act))
	})

	t.Run("match complete returns nil", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		extra := state.Extra.(*State)
		extra.Stage = StageHandOver
		extra.MatchComplete = true
		assert.Nil(t, rules.TimeoutAction(state))
	})
}

// The engine broadcasts a play after AfterAction returns, so a trick swept the
// moment its fourth card lands is gone before anyone is told about it: the three
// players who did not take it never see what beat them. It has to stay on the table
// until somebody leads the next one.
func TestRules_FinishedTrickStaysOnTheTableUntilTheNextLead(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	// Everyone follows clubs; p3 takes the trick with the king.
	state := createTestState(
		[]deck.Card{{Rank: deck.Five, Suit: deck.Clubs}},
		[]deck.Card{{Rank: deck.Three, Suit: deck.Clubs}},
		[]deck.Card{{Rank: deck.King, Suit: deck.Clubs}, {Rank: deck.Nine, Suit: deck.Diamonds}},
		[]deck.Card{{Rank: deck.Two, Suit: deck.Clubs}},
	)
	extra := state.Extra.(*State)
	extra.TricksPlayed = 1 // past the opening trick, so the 2♣ lead rule is done

	for seat, p := range state.Players {
		state.CurrentTurn = seat
		card := p.Cards[0]
		require.NoError(t, rules.ValidateAction(state, ActionPlayCard{Card: card}))
		rules.ApplyAction(state, ActionPlayCard{Card: card})
		require.NoError(t, rules.AfterAction(state, ActionPlayCard{Card: card}))
	}

	require.Equal(t, "p3", extra.LastTrickWinner)
	assert.True(t, extra.TrickComplete)
	assert.Len(t, extra.TrickCards, playerCount, "all four cards are still up for the broadcast")
	assert.Equal(t, deck.Clubs, extra.LedSuit, "and the suit that was led still reads correctly")

	// The winner leads the next trick. A won trick is not one in progress, so an
	// off-suit lead is a lead, not a failure to follow suit.
	next := deck.Card{Rank: deck.Nine, Suit: deck.Diamonds}
	require.NoError(t, rules.ValidateAction(state, ActionPlayCard{Card: next}))
	rules.ApplyAction(state, ActionPlayCard{Card: next})

	assert.False(t, extra.TrickComplete)
	assert.Equal(t, map[string]deck.Card{"p3": next}, extra.TrickCards, "the table was swept for the new trick")
	assert.Equal(t, deck.Diamonds, extra.LedSuit)
}

func TestRules_OnPlayerLeave_EndsMatch(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	state := createTestState()
	rules.OnPlayerLeave(state, "p2")
	assert.True(t, state.Extra.(*State).MatchComplete)
	assert.True(t, rules.CheckWinCondition(state))
}

func TestSmoke_FullHandConservesTheDeck(t *testing.T) {
	t.Parallel()
	engine := game.NewEngine(&Rules{}, fourPlayers(), deck.StandardDeck())
	require.NoError(t, engine.Start())
	t.Cleanup(engine.Close)

	// Drive the opening pass (or skip if hand 1 somehow had PassNone — it won't).
	for {
		var stage Stage
		engine.WithState(func(s *game.State) {
			stage = s.Extra.(*State).Stage
		})
		if stage != StagePassing {
			break
		}
		id := engine.CurrentPlayerID()
		var cards []deck.Card
		engine.WithState(func(s *game.State) {
			for _, p := range s.Players {
				if p.ID == id {
					cards = threeLowestCards(p.Cards)
					break
				}
			}
		})
		require.NoError(t, engine.SubmitAction(id, ActionPassCards{Cards: cards}))
	}

	for range cardsPerHand {
		for range playerCount {
			id := engine.CurrentPlayerID()
			var act game.Action
			engine.WithState(func(s *game.State) {
				extra := s.Extra.(*State)
				p := s.Players[s.CurrentTurn]
				card, ok := firstLegalCard(s, extra, p)
				require.True(t, ok, "seat %s must have a legal card", id)
				act = ActionPlayCard{Card: card}
			})
			require.NoError(t, engine.SubmitAction(id, act))
		}
	}

	engine.WithState(func(s *game.State) {
		extra := s.Extra.(*State)
		assert.Equal(t, StageHandOver, extra.Stage)
		assert.Equal(t, cardsPerHand, extra.TricksPlayed)
		total := 0
		for _, pts := range extra.HandPoints {
			total += pts
		}
		assert.Equal(t, penaltyPointsTotal, total)
		inHands := 0
		for _, p := range s.Players {
			inHands += len(p.Cards)
		}
		assert.Zero(t, inHands)
	})
}
