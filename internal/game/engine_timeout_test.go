package game

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/player"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type namedAction struct{ name string }

func (a namedAction) Name() string { return a.name }

// timeoutRules is the smallest Rules that opts into the turn clock. It accepts every
// action and records who played what, so a test can tell an auto-played move from a
// real one.
type timeoutRules struct {
	// safe is what TimeoutAction returns. A nil safe means the rules have no safe
	// move, which is how the engine is told to take the seat instead.
	safe Action
	// reject makes ValidateAction refuse everything, standing in for rules that
	// cannot accept their own safe move.
	reject bool

	// mu guards applied only against the test goroutine: the engine calls ApplyAction
	// under its own locks, but the test reads the slice from outside them.
	mu      sync.Mutex
	applied []string
}

func (r *timeoutRules) MinPlayers() int                  { return 2 }
func (r *timeoutRules) MaxPlayers() int                  { return 4 }
func (r *timeoutRules) InitialDeck() []deck.Card         { return deck.StandardDeck() }
func (r *timeoutRules) InitialDealCount() int            { return 1 }
func (r *timeoutRules) OnGameStart(*State) error         { return nil }
func (r *timeoutRules) AfterAction(*State, Action) error { return nil }
func (r *timeoutRules) CheckWinCondition(*State) bool    { return false }

func (r *timeoutRules) ValidateAction(_ *State, _ Action) error {
	if r.reject {
		return errors.New("rules reject everything")
	}
	return nil
}

func (r *timeoutRules) ApplyAction(state *State, action Action) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.applied = append(r.applied, state.Players[state.CurrentTurn].ID+":"+action.Name())
}

func (r *timeoutRules) Standings(state *State) []*player.Player { return state.Players }

func (r *timeoutRules) TimeoutAction(*State) Action { return r.safe }

func (r *timeoutRules) appliedActions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.applied...)
}

var _ TurnTimeoutHandler = (*timeoutRules)(nil)

// fireTurnTimeout runs the expiry path synchronously with the sequence the armed
// timer is holding. Driving it directly is what keeps these tests off the wall clock:
// the real timer is covered separately by TestEngine_TurnTimeout_TimerActuallyFires.
func fireTurnTimeout(t *testing.T, e *Engine) {
	t.Helper()
	e.mu.Lock()
	seq := e.turnSeq
	e.mu.Unlock()
	e.onTurnTimeout(seq)
}

func newTimeoutEngine(t *testing.T, rules Rules, ids ...string) *Engine {
	t.Helper()
	players := make([]*player.Player, 0, len(ids))
	for _, id := range ids {
		players = append(players, &player.Player{ID: id})
	}
	// A long timeout so nothing fires on its own; the tests trigger expiry themselves.
	engine := NewEngine(rules, players, deck.StandardDeck(), WithTurnTimeout(time.Hour))
	t.Cleanup(engine.Close)
	require.NoError(t, engine.Start())
	return engine
}

func TestEngine_TurnTimeout_PlaysTheSafeMove(t *testing.T) {
	t.Parallel()
	rules := &timeoutRules{safe: namedAction{name: "safe"}}
	engine := newTimeoutEngine(t, rules, "a", "b")

	before := engine.CurrentPlayerID()
	fireTurnTimeout(t, engine)

	assert.Equal(t, []string{before + ":safe"}, rules.appliedActions(),
		"the expired seat's safe move must be played for them")
	assert.Equal(t, 1, engine.MissedTurns(before))
	assert.NotEqual(t, before, engine.CurrentPlayerID(), "the turn must move on")
}

func TestEngine_TurnTimeout_OwnActionClearsTheCount(t *testing.T) {
	t.Parallel()
	rules := &timeoutRules{safe: namedAction{name: "safe"}}
	engine := newTimeoutEngine(t, rules, "a", "b")

	first := engine.CurrentPlayerID()
	fireTurnTimeout(t, engine)
	second := engine.CurrentPlayerID()
	fireTurnTimeout(t, engine)
	require.Equal(t, first, engine.CurrentPlayerID(), "two seats, so the turn is back")
	require.Equal(t, 1, engine.MissedTurns(first))

	require.NoError(t, engine.SubmitAction(first, namedAction{name: "real"}))

	assert.Zero(t, engine.MissedTurns(first), "playing resets the idle count")
	assert.Equal(t, 1, engine.MissedTurns(second), "and does not touch anybody else's")
}

func TestEngine_TurnTimeout_TakesTheSeatAfterMaxMissesInARow(t *testing.T) {
	t.Parallel()
	rules := &timeoutRules{safe: namedAction{name: "safe"}}
	engine := newTimeoutEngine(t, rules, "a", "b")
	events, err := engine.Broadcaster().Subscribe()
	require.NoError(t, err)

	firstToAct := engine.CurrentPlayerID()
	// Two seats alternating, so the first to act is also the first to reach the limit.
	for range MaxMissedTurns * 2 {
		if engine.IsFinished() {
			break
		}
		fireTurnTimeout(t, engine)
	}

	require.True(t, engine.IsFinished(), "losing a seat leaves one player, which ends the game")
	assert.Equal(t, MaxMissedTurns, engine.MissedTurns(firstToAct))

	var idleFor string
	for {
		select {
		case ev := <-events:
			if ev.Type == EventPlayerIdle {
				idleFor = ev.PlayerID
			}
			continue
		default:
		}
		break
	}
	assert.Equal(t, firstToAct, idleFor, "the idle player must be named so their session can end")

	// Only the misses before the limit are played out; the last one takes the seat.
	assert.Len(t, rules.appliedActions(), (MaxMissedTurns-1)*2)
}

func TestEngine_TurnTimeout_StaleTimerIsIgnored(t *testing.T) {
	t.Parallel()
	rules := &timeoutRules{safe: namedAction{name: "safe"}}
	engine := newTimeoutEngine(t, rules, "a", "b")

	// Capture the sequence the armed timer holds, then act: this is the player who
	// moved in the instant before their own clock ran out.
	engine.mu.Lock()
	stale := engine.turnSeq
	engine.mu.Unlock()

	acted := engine.CurrentPlayerID()
	require.NoError(t, engine.SubmitAction(acted, namedAction{name: "real"}))

	engine.onTurnTimeout(stale)

	assert.Equal(t, []string{acted + ":real"}, rules.appliedActions(),
		"a timeout for a turn that is over must not play anything")
	assert.Zero(t, engine.MissedTurns(acted), "and must not be counted against them")
}

func TestEngine_TurnTimeout_NoSafeMoveTakesTheSeat(t *testing.T) {
	t.Parallel()
	rules := &timeoutRules{safe: nil}
	engine := newTimeoutEngine(t, rules, "a", "b")

	fireTurnTimeout(t, engine)

	assert.Empty(t, rules.appliedActions(), "there was no safe move to play")
	assert.True(t, engine.IsFinished(), "the seat goes rather than the table waiting")
}

func TestEngine_TurnTimeout_RefusedSafeMoveRearmsRatherThanStalling(t *testing.T) {
	t.Parallel()
	rules := &timeoutRules{safe: namedAction{name: "safe"}, reject: true}
	engine := newTimeoutEngine(t, rules, "a", "b")

	stuck := engine.CurrentPlayerID()
	fireTurnTimeout(t, engine)

	assert.Empty(t, rules.appliedActions(), "validation refused it")
	assert.Equal(t, 1, engine.MissedTurns(stuck), "the miss still counts")
	assert.Equal(t, stuck, engine.CurrentPlayerID(), "the turn could not advance")
	assert.False(t, engine.TurnDeadline().IsZero(),
		"a fresh clock must be running, or this seat stalls the table forever")
}

// The rearm above is only observable through a real timer: driving onTurnTimeout directly
// Only a real timer shows the rearm: driving onTurnTimeout directly supplies a fresh
// sequence every call, so a seat that never got a new clock still looks charged.
func TestEngine_TurnTimeout_RefusedSafeMoveStillLosesTheSeat(t *testing.T) {
	t.Parallel()
	rules := &timeoutRules{safe: namedAction{name: "safe"}, reject: true}
	engine := NewEngine(rules, []*player.Player{{ID: "a"}, {ID: "b"}},
		deck.StandardDeck(), WithTurnTimeout(20*time.Millisecond))
	t.Cleanup(engine.Close)
	require.NoError(t, engine.Start())

	require.Eventually(t, engine.IsFinished, 5*time.Second, 5*time.Millisecond,
		"each refusal must re-arm the clock until the idle seat is taken")
	assert.Empty(t, rules.appliedActions(), "nothing was ever accepted")
}

func TestEngine_TurnTimeout_ClockLifecycle(t *testing.T) {
	t.Parallel()

	t.Run("rules without a safe move get no clock", func(t *testing.T) {
		t.Parallel()
		rules := setupMockRules()
		rules.On("CheckWinCondition", mock.Anything).Return(false).Maybe()
		engine := NewEngine(rules, []*player.Player{{ID: "a"}, {ID: "b"}}, deck.StandardDeck())
		t.Cleanup(engine.Close)
		require.NoError(t, engine.Start())

		assert.True(t, engine.TurnDeadline().IsZero(),
			"a game with no safe move must not arm a clock it cannot honor")
	})

	t.Run("close stops the clock", func(t *testing.T) {
		t.Parallel()
		engine := newTimeoutEngine(t, &timeoutRules{safe: namedAction{name: "safe"}}, "a", "b")
		require.False(t, engine.TurnDeadline().IsZero())

		engine.Close()

		assert.True(t, engine.TurnDeadline().IsZero())
	})

	t.Run("a finished game stops the clock", func(t *testing.T) {
		t.Parallel()
		engine := newTimeoutEngine(t, &timeoutRules{safe: nil}, "a", "b")
		require.False(t, engine.TurnDeadline().IsZero())

		fireTurnTimeout(t, engine) // no safe move, so this finishes the game
		require.True(t, engine.IsFinished())

		assert.True(t, engine.TurnDeadline().IsZero(),
			"a finished game must not auto-play")
	})

	t.Run("a zero timeout disables the clock", func(t *testing.T) {
		t.Parallel()
		rules := &timeoutRules{safe: namedAction{name: "safe"}}
		engine := NewEngine(rules, []*player.Player{{ID: "a"}, {ID: "b"}},
			deck.StandardDeck(), WithTurnTimeout(0))
		t.Cleanup(engine.Close)
		require.NoError(t, engine.Start())

		assert.True(t, engine.TurnDeadline().IsZero())
	})
}

// Every other test here drives onTurnTimeout directly, which would keep passing even if the
// timer were never armed.
func TestEngine_TurnTimeout_TimerActuallyFires(t *testing.T) {
	t.Parallel()
	rules := &timeoutRules{safe: namedAction{name: "safe"}}
	engine := NewEngine(rules, []*player.Player{{ID: "a"}, {ID: "b"}},
		deck.StandardDeck(), WithTurnTimeout(20*time.Millisecond))
	t.Cleanup(engine.Close)
	require.NoError(t, engine.Start())

	require.Eventually(t, func() bool {
		return len(rules.appliedActions()) > 0
	}, 2*time.Second, 5*time.Millisecond, "the armed timer must play the safe move on its own")
}
