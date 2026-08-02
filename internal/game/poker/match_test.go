package poker

import (
	"fmt"
	"maps"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/player"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatch_ChipsAndButtonCarryIntoTheNextHand(t *testing.T) {
	t.Parallel()
	engine := startTable(t, 3)
	t.Cleanup(engine.Close)

	before := extraOf(t, engine)
	firstDealer := before.DealerIndex

	// Fold the table down to one player, ending hand one.
	for range 3 {
		if extraOf(t, engine).HandComplete {
			break
		}
		require.NoError(t, engine.SubmitAction(engine.CurrentPlayerID(), ActionFold{}))
	}

	won := extraOf(t, engine)
	require.True(t, won.HandComplete)
	require.False(t, engine.IsFinished())
	stacks := maps.Clone(won.PlayerChips)

	require.NoError(t, engine.SubmitAction(engine.CurrentPlayerID(), ActionNextHand{}))

	next := extraOf(t, engine)
	assert.Equal(t, 2, next.HandNumber)
	assert.NotEqual(t, firstDealer, next.DealerIndex, "the button moves on between hands")
	for id, want := range stacks {
		// Only the two blinds have paid anything into the new hand.
		assert.Equal(t, want, next.PlayerChips[id]+next.PlayerBets[id],
			"player %s must start the hand with what they finished the last one with", id)
	}

	engine.WithState(func(s *game.State) {
		for _, p := range s.Players {
			assert.Len(t, p.Cards, 2, "every funded player is dealt a fresh hand")
		}
	})
}

// Walking out mid-match forfeits it, however far ahead the leaver was: they place
// below everyone who saw the match through. Between themselves, leavers are still
// ranked on the chips they won, not on who quit first.
func TestStandings_LeavingForfeitsTheMatchButNotTheChipsWon(t *testing.T) {
	t.Parallel()
	engine := startTable(t, 3)
	t.Cleanup(engine.Close)

	engine.WithState(func(s *game.State) {
		extra, ok := s.Extra.(*State)
		require.True(t, ok)
		// p2 is well clear of the table, as if they had taken a couple of hands.
		extra.PlayerChips["p1"] = 700
		extra.PlayerChips["p2"] = 1600
		extra.PlayerChips["p3"] = 700
	})

	engine.RemovePlayer("p2")
	assert.Equal(t, []string{"p1", "p3", "p2"}, engine.StandingsIDs(),
		"the chip leader drops behind both players still at the table")

	engine.RemovePlayer("p3")
	assert.Equal(t, []string{"p1", "p2", "p3"}, engine.StandingsIDs(),
		"between leavers the bigger stack still places higher")

	engine.WithState(func(s *game.State) {
		require.NotNil(t, s.Winner)
		assert.Equal(t, "p1", s.Winner.ID, "the last player standing wins the match")
	})
}

func TestMatch_NextHandRejectedWhileTheHandIsLive(t *testing.T) {
	t.Parallel()
	engine := startTable(t, 2)
	t.Cleanup(engine.Close)

	err := engine.SubmitAction(engine.CurrentPlayerID(), ActionNextHand{})
	assert.ErrorContains(t, err, "still being played")
}

func TestFinishHand(t *testing.T) {
	t.Parallel()

	t.Run("parks the turn on the next dealer", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		extra := state.Extra.(*State)
		extra.HandNumber, extra.HandsTotal = 1, HandsPerMatch
		extra.DealerIndex = 0

		finishHand(state, extra)

		assert.False(t, extra.MatchComplete)
		require.NotNil(t, state.OverrideNextTurn)
		assert.Equal(t, 1, *state.OverrideNextTurn, "seat after the button deals next")
	})

	t.Run("ends the match once the hands run out", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		extra := state.Extra.(*State)
		extra.HandNumber, extra.HandsTotal = HandsPerMatch, HandsPerMatch

		finishHand(state, extra)

		assert.True(t, extra.MatchComplete)
		assert.True(t, (&Rules{}).CheckWinCondition(state))
	})

	t.Run("ends the match once one player holds every chip", func(t *testing.T) {
		t.Parallel()
		state := createTestState()
		extra := state.Extra.(*State)
		extra.HandNumber, extra.HandsTotal = 1, HandsPerMatch
		extra.PlayerChips = map[string]uint{"p1": 3000, "p2": 0, "p3": 0}

		finishHand(state, extra)

		assert.True(t, extra.MatchComplete)
		assert.Equal(t, "p1", (&Rules{}).Standings(state)[0].ID, "the biggest stack wins the match")
	})
}

// tableWithChips seats one player per stack, in order, ready for beginHand.
func tableWithChips(stacks ...uint) (*game.State, *State) {
	players := make([]*player.Player, 0, len(stacks))
	extra := &State{
		SmallBlind:       DefaultSmallBlind,
		BigBlind:         DefaultBigBlind,
		HandsTotal:       HandsPerMatch,
		Folded:           map[string]bool{},
		PlayersAllIn:     map[string]bool{},
		Table:            make([]deck.Card, 0, 5),
		PlayerChips:      map[string]uint{},
		PlayerBets:       map[string]uint{},
		TotalContributed: map[string]uint{},
		ActedThisRound:   map[string]bool{},
	}
	for i, chips := range stacks {
		id := fmt.Sprintf("p%d", i)
		players = append(players, &player.Player{ID: id})
		extra.PlayerChips[id] = chips
	}
	state := game.NewState(&Rules{}, players, deck.StandardDeck())
	state.Extra = extra
	state.Phase = game.Playing
	return state, extra
}

// Counting funded seats after the blinds are posted makes a full table where the
// blinds bust two short stacks look heads-up, which hands the button first action
// instead of the seat under the gun.
func TestBeginHand_ShortBlindsDoNotMakeTheTableLookHeadsUp(t *testing.T) {
	t.Parallel()
	// Seat 0 has the button, so seat 1 posts the small blind and seat 2 the big
	// blind - both smaller than the blind they owe, so both are all-in on posting.
	state, extra := tableWithChips(1000, 20, 30, 1470)

	require.NoError(t, (&Rules{}).beginHand(state, extra, 0))

	assert.Equal(t, 1, extra.SBIndex)
	assert.Equal(t, 2, extra.BBIndex)
	assert.Equal(t, 3, state.CurrentTurn, "the seat after the big blind is under the gun, not the button")
}

func TestStandings_BustedPlayersRankByHowLongTheyLasted(t *testing.T) {
	t.Parallel()
	state, extra := tableWithChips(3000, 0, 0)
	extra.HandNumber, extra.HandsTotal = HandsPerMatch, HandsPerMatch
	// p1 went out early, p2 survived nearly to the end. Both finish on zero chips,
	// so nothing but the bust-out hand can separate them.
	extra.BustedAtHand = map[string]int{"p1": 2, "p2": 9}

	standings := (&Rules{}).Standings(state)

	assert.Equal(t, []string{"p0", "p2", "p1"},
		[]string{standings[0].ID, standings[1].ID, standings[2].ID})
}

func TestFinishHand_StampsTheHandAPlayerWentOutOn(t *testing.T) {
	t.Parallel()
	state, extra := tableWithChips(3000, 0, 500)
	extra.HandNumber, extra.HandsTotal = 4, HandsPerMatch

	finishHand(state, extra)

	assert.Equal(t, map[string]int{"p1": 4}, extra.BustedAtHand)

	// A later hand must not restamp a player who was already out.
	extra.HandNumber = 5
	extra.PlayerChips["p2"] = 0
	finishHand(state, extra)
	assert.Equal(t, map[string]int{"p1": 4, "p2": 5}, extra.BustedAtHand)
}

// An uncontested pot is won face-down. With hands left to play, showing those
// cards would hand the rest of the table a free read.
func TestFoldedOutHand_IsNotShownDown(t *testing.T) {
	t.Parallel()
	engine := startTable(t, 3)
	t.Cleanup(engine.Close)

	for range 3 {
		if extraOf(t, engine).HandComplete {
			break
		}
		require.NoError(t, engine.SubmitAction(engine.CurrentPlayerID(), ActionFold{}))
	}

	extra := extraOf(t, engine)
	require.True(t, extra.HandComplete)
	assert.False(t, extra.ReachedShowdown, "nobody called, so nobody has to show")
}

func TestShowdown_MarksTheHandAsShownDown(t *testing.T) {
	t.Parallel()
	engine := startTable(t, 3)
	t.Cleanup(engine.Close)

	for range 10 {
		if extraOf(t, engine).HandComplete {
			break
		}
		require.NoError(t, engine.SubmitAction(engine.CurrentPlayerID(), ActionAllIn{}))
	}

	extra := extraOf(t, engine)
	require.True(t, extra.HandComplete)
	assert.True(t, extra.ReachedShowdown, "an all-in board that runs out is shown down")
}

func TestBeginHand_BustedPlayerSitsOut(t *testing.T) {
	t.Parallel()
	state := createTestState()
	extra := state.Extra.(*State)
	extra.HandsTotal = HandsPerMatch
	extra.PlayerChips["p2"] = 0

	require.NoError(t, (&Rules{}).beginHand(state, extra, 0))

	assert.True(t, extra.Folded["p2"], "a busted player is folded for the rest of the match")
	assert.Empty(t, state.Players[1].Cards, "a busted player is not dealt in")
	assert.NotEqual(t, 1, extra.SBIndex)
	assert.NotEqual(t, 1, extra.BBIndex)
	assert.NotEqual(t, 1, state.CurrentTurn, "the turn cursor skips the empty seat")
}
