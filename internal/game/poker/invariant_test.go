package poker

import (
	"fmt"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/player"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// chipsInPlay is the invariant every betting path must preserve: chips only ever
// move between a player's stack and the pool, so the two together are constant for
// the whole hand. A pot that pays out more or less than it collected shows up here.
func chipsInPlay(extra *State) uint {
	total := extra.MainPool
	for _, c := range extra.PlayerChips {
		total += c
	}
	return total
}

func startTable(t *testing.T, n int) *game.Engine {
	t.Helper()
	players := make([]*player.Player, 0, n)
	for i := range n {
		players = append(players, &player.Player{ID: fmt.Sprintf("p%d", i+1)})
	}
	engine := game.NewEngine(&Rules{}, players, deck.StandardDeck())
	require.NoError(t, engine.Start())
	return engine
}

// candidateActions is every action worth attempting, cheapest first. Illegal ones
// are rejected by the engine, so the driver can just try the next.
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
		engine := startTable(t, n)
		want := uint(n) * DefaultStack

		require.Equal(t, want, chipsInPlay(extraOf(t, engine)), "blinds must not create or destroy chips")

		for step := range 200 {
			if engine.IsFinished() {
				break
			}
			extra := extraOf(t, engine)
			if extra.HandComplete {
				break
			}

			// Bias toward a legal raise size so the raise path is actually exercised.
			raiseTo := extra.CurrentBet + extra.MinRaise +
				uint(rapid.IntRange(0, 200).Draw(rt, fmt.Sprintf("raise%d", step)))
			order := rapid.IntRange(0, 4).Draw(rt, fmt.Sprintf("pick%d", step))

			actions := candidateActions(raiseTo)
			id := engine.CurrentPlayerID()
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

			assert.Equal(t, want, chipsInPlay(extraOf(t, engine)),
				"chips changed after step %d (%d players)", step, n)
		}

		// However the hand ended, every chip must be back in a stack.
		final := extraOf(t, engine)
		if final.HandComplete {
			assert.Zero(t, final.MainPool, "a completed hand must leave nothing in the pool")
			var stacks uint
			for _, c := range final.PlayerChips {
				stacks += c
			}
			assert.Equal(t, want, stacks, "payouts must return exactly the chips collected")
		}
	})
}

// Everyone all-in before the flop: no betting can continue, so the board has to run
// out to the river and pay out. runOutBoard had no direct coverage.
func TestAllInPreflop_RunsOutBoardAndPays(t *testing.T) {
	t.Parallel()
	engine := startTable(t, 3)
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

	engine.WithState(func(s *game.State) {
		extra, ok := s.Extra.(*State)
		require.True(t, ok)
		extra.PlayerChips[s.Players[0].ID] = 100
		extra.PlayerChips[s.Players[1].ID] = 500
		extra.PlayerChips[s.Players[2].ID] = 900
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
