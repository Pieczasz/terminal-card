package ginrummy

import (
	"testing"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/player"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatch_ScoresCarryIntoNextHand(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	state, extra := startedState(t)

	knocker := []deck.Card{
		c(deck.Two, deck.Hearts), c(deck.Three, deck.Hearts), c(deck.Four, deck.Hearts), c(deck.Five, deck.Hearts),
		c(deck.Jack, deck.Spades), c(deck.Jack, deck.Hearts), c(deck.Jack, deck.Diamonds),
		c(deck.Ace, deck.Clubs), c(deck.Ace, deck.Spades), c(deck.Ace, deck.Hearts),
		c(deck.King, deck.Clubs),
	}
	opponent := []deck.Card{
		c(deck.King, deck.Spades), c(deck.Queen, deck.Hearts), c(deck.Nine, deck.Diamonds),
		c(deck.Eight, deck.Clubs), c(deck.Seven, deck.Spades), c(deck.Six, deck.Hearts),
		c(deck.Three, deck.Clubs), c(deck.Two, deck.Diamonds), c(deck.Four, deck.Spades),
		c(deck.Five, deck.Diamonds),
	}
	state.Players[0].Cards = knocker
	state.Players[1].Cards = opponent
	extra.HandPhase = AwaitingDiscard
	state.CurrentTurn = 0

	rules.ApplyAction(state, ActionKnock{Discard: c(deck.King, deck.Clubs)})
	score := extra.CumulativeScores["p1"]
	require.Positive(t, score)
	require.True(t, extra.HandComplete)

	require.NoError(t, rules.ValidateAction(state, ActionNextHand{}))
	require.NoError(t, rules.AfterAction(state, ActionNextHand{}))
	assert.Equal(t, score, extra.CumulativeScores["p1"], "scores carry across hands")
	assert.Equal(t, 2, extra.HandNumber)
	assert.False(t, extra.HandComplete)
	assert.Equal(t, AwaitingDraw, extra.HandPhase)
}

func TestMatch_NextHandRejectedWhileHandLive(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	state, _ := startedState(t)
	err := rules.ValidateAction(state, ActionNextHand{})
	require.ErrorContains(t, err, "still being played")
}

func TestMatch_EndsOnceCumulativeScoreCrossesTarget(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	state, extra := startedState(t)
	extra.CumulativeScores["p1"] = TargetScore - 1

	knocker := []deck.Card{
		c(deck.Two, deck.Hearts), c(deck.Three, deck.Hearts), c(deck.Four, deck.Hearts), c(deck.Five, deck.Hearts),
		c(deck.Jack, deck.Spades), c(deck.Jack, deck.Hearts), c(deck.Jack, deck.Diamonds),
		c(deck.Ace, deck.Clubs), c(deck.Ace, deck.Spades), c(deck.Ace, deck.Hearts),
		c(deck.King, deck.Clubs),
	}
	opponent := []deck.Card{
		c(deck.King, deck.Spades), c(deck.Queen, deck.Hearts), c(deck.Nine, deck.Diamonds),
		c(deck.Eight, deck.Clubs), c(deck.Seven, deck.Spades), c(deck.Six, deck.Hearts),
		c(deck.Three, deck.Clubs), c(deck.Two, deck.Diamonds), c(deck.Four, deck.Spades),
		c(deck.Five, deck.Diamonds),
	}
	state.Players[0].Cards = knocker
	state.Players[1].Cards = opponent
	extra.HandPhase = AwaitingDiscard
	state.CurrentTurn = 0

	rules.ApplyAction(state, ActionKnock{Discard: c(deck.King, deck.Clubs)})
	assert.True(t, extra.MatchComplete)
	assert.True(t, rules.CheckWinCondition(state))
	assert.GreaterOrEqual(t, extra.CumulativeScores["p1"], TargetScore)
}

func TestStandings_LeavingForfeitsScoresStay(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	players := []*player.Player{
		{ID: "p1", DatabaseUser: nil},
		{ID: "p2", DatabaseUser: nil},
	}
	engine := game.NewEngine(rules, players, deck.StandardDeck())
	require.NoError(t, engine.Start())
	t.Cleanup(engine.Close)

	var scoreBefore map[string]int
	engine.WithState(func(s *game.State) {
		extra := s.Extra.(*State)
		extra.CumulativeScores["p1"] = 30
		extra.CumulativeScores["p2"] = 12
		scoreBefore = map[string]int{"p1": 30, "p2": 12}
	})

	engine.RemovePlayer("p2")
	assert.Equal(t, game.Finished, engine.Snapshot().Phase)
	standings := engine.Standings()
	require.NotEmpty(t, standings)
	assert.Equal(t, "p1", standings[0].ID)

	engine.WithState(func(s *game.State) {
		extra := s.Extra.(*State)
		assert.Equal(t, scoreBefore["p1"], extra.CumulativeScores["p1"])
		assert.Equal(t, scoreBefore["p2"], extra.CumulativeScores["p2"])
	})
}

func TestMatch_FirstActorAlternates(t *testing.T) {
	t.Parallel()
	rules := &Rules{}
	state, extra := startedState(t)
	assert.Equal(t, 0, extra.FirstActor)

	extra.HandComplete = true
	extra.FirstActor = 0
	next := 1
	extra.FirstActor = next
	state.CurrentTurn = next
	require.NoError(t, rules.AfterAction(state, ActionNextHand{}))
	assert.Equal(t, 1, extra.FirstActor)
	assert.Equal(t, 1, state.CurrentTurn)
}
