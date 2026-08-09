package game

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/broadcaster"
	"github.com/Pieczasz/terminal-card/internal/deck"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockRules struct {
	mock.Mock
}

func (m *MockRules) MinPlayers() int {
	args := m.Called()
	return args.Int(0)
}

func (m *MockRules) MaxPlayers() int {
	args := m.Called()
	return args.Int(0)
}

func (m *MockRules) InitialDeck() []deck.Card {
	args := m.Called()
	return args.Get(0).([]deck.Card)
}

func (m *MockRules) InitialDealCount() int {
	args := m.Called()
	return args.Int(0)
}

func (m *MockRules) OnGameStart(state *State) error {
	args := m.Called(state)
	return args.Error(0)
}

func (m *MockRules) ValidateAction(state *State, action Action) error {
	args := m.Called(state, action)
	return args.Error(0)
}

func (m *MockRules) ApplyAction(state *State, action Action) {
	m.Called(state, action)
}

func (m *MockRules) AfterAction(state *State, action Action) error {
	args := m.Called(state, action)
	return args.Error(0)
}

func (m *MockRules) CheckWinCondition(state *State) bool {
	args := m.Called(state)
	return args.Bool(0)
}

func (m *MockRules) Standings(state *State) []*Player {
	args := m.Called(state)
	return args.Get(0).([]*Player)
}

func setupMockRules() *MockRules {
	m := new(MockRules)
	m.On("InitialDeck").Return(deck.StandardDeck()).Maybe()
	m.On("InitialDealCount").Return(5)
	m.On("OnGameStart", mock.Anything).Return(nil)
	return m
}

func TestEngine_Start(t *testing.T) {
	t.Parallel()
	players := []*Player{{ID: "p1"}, {ID: "p2"}}
	m := setupMockRules()
	engine := NewEngine(m, players, deck.StandardDeck())

	err := engine.Start()
	require.NoError(t, err)

	engine.WithState(func(state *State) {
		assert.Equal(t, Playing, state.Phase)
		assert.Len(t, state.Players[0].Cards, 5)
		assert.Len(t, state.Players[1].Cards, 5)
	})

	m.AssertExpectations(t)
}

// A start that fails must leave a table somebody can start again, not one parked in a
// phase no player can act in.
func TestEngine_Start_FailureLeavesTheTableStartable(t *testing.T) {
	t.Parallel()

	t.Run("a second start is refused", func(t *testing.T) {
		t.Parallel()
		engine := newStartedEngine(t, "p1", "p2")

		require.ErrorContains(t, engine.Start(), "already started")
		engine.WithState(func(state *State) { assert.Equal(t, Playing, state.Phase) })
	})

	t.Run("too few cards to deal", func(t *testing.T) {
		t.Parallel()
		m := setupMockRules()
		engine := NewEngine(m, []*Player{{ID: "p1"}, {ID: "p2"}}, deck.StandardDeck()[:4])
		t.Cleanup(engine.Close)

		require.ErrorContains(t, engine.Start(), "insufficient number of cards")
		engine.WithState(func(state *State) {
			assert.Equal(t, Waiting, state.Phase)
			assert.Empty(t, state.Players[0].Cards, "a deal that could not finish deals nobody in")
		})
	})

	t.Run("rules that cannot set the game up", func(t *testing.T) {
		t.Parallel()
		m := new(MockRules)
		m.On("InitialDealCount").Return(5)
		m.On("OnGameStart", mock.Anything).Return(assert.AnError)
		engine := NewEngine(m, []*Player{{ID: "p1"}, {ID: "p2"}}, deck.StandardDeck())
		t.Cleanup(engine.Close)

		require.ErrorContains(t, engine.Start(), "failed to setup game")
		engine.WithState(func(state *State) { assert.Equal(t, Waiting, state.Phase) })
	})
}

type MockAction struct {
	name string
}

func (m MockAction) Name() string {
	return m.name
}

func TestEngine_SubmitAction(t *testing.T) {
	t.Parallel()
	players := []*Player{{ID: "p1"}, {ID: "p2"}}
	m := setupMockRules()
	engine := NewEngine(m, players, deck.StandardDeck())
	require.NoError(t, engine.Start())

	currentPlayerID := engine.CurrentPlayerID()
	otherPlayerID := "p2"
	if currentPlayerID == "p2" {
		otherPlayerID = "p1"
	}

	err := engine.SubmitAction(otherPlayerID, MockAction{name: "MockDraw"})
	require.ErrorContains(t, err, "wait for your turn")

	validAction := MockAction{name: "MockDraw"}
	m.On("ValidateAction", mock.Anything, validAction).Return(nil)
	m.On("ApplyAction", mock.Anything, validAction)
	m.On("AfterAction", mock.Anything, validAction).Return(nil)
	m.On("CheckWinCondition", mock.Anything).Return(false)

	err = engine.SubmitAction(currentPlayerID, validAction)
	require.NoError(t, err)

	assert.Equal(t, otherPlayerID, engine.CurrentPlayerID())

	m.AssertExpectations(t)
}

func TestEngine_SubmitAction_SetsWinnerFromStandings(t *testing.T) {
	t.Parallel()
	winner := &Player{ID: "p2"}
	loser := &Player{ID: "p1"}
	players := []*Player{loser, winner}

	m := setupMockRules()
	engine := NewEngine(m, players, deck.StandardDeck())
	require.NoError(t, engine.Start())

	currentPlayerID := engine.CurrentPlayerID()
	action := MockAction{name: "Win"}
	m.On("ValidateAction", mock.Anything, action).Return(nil)
	m.On("ApplyAction", mock.Anything, action)
	m.On("AfterAction", mock.Anything, action).Return(nil)
	m.On("CheckWinCondition", mock.Anything).Return(true)
	m.On("Standings", mock.Anything).Return([]*Player{winner, loser})

	err := engine.SubmitAction(currentPlayerID, action)
	require.NoError(t, err)

	engine.WithState(func(state *State) {
		assert.Equal(t, Finished, state.Phase)
		assert.Equal(t, "p2", state.Winner.ID)
	})
}

func TestEngine_SubmitAction_PostConditionBeforeBroadcast(t *testing.T) {
	t.Parallel()
	players := []*Player{{ID: "p1"}, {ID: "p2"}}
	m := setupMockRules()
	engine := NewEngine(m, players, deck.StandardDeck())

	ch, subErr := engine.Broadcaster().Subscribe()
	require.NoError(t, subErr)
	t.Cleanup(func() { engine.Broadcaster().Unsubscribe(ch) })

	require.NoError(t, engine.Start())

	// Drain start event
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for start event")
	}

	currentPlayerID := engine.CurrentPlayerID()
	action := MockAction{name: "Bad"}
	m.On("ValidateAction", mock.Anything, action).Return(nil)
	m.On("ApplyAction", mock.Anything, action)
	m.On("AfterAction", mock.Anything, action).Return(assert.AnError)
	m.On("Standings", mock.Anything).Return([]*Player{players[0], players[1]})

	err := engine.SubmitAction(currentPlayerID, action)
	require.Error(t, err)

	engine.WithState(func(state *State) {
		assert.Equal(t, Finished, state.Phase)
	})

	// Broadcast is synchronous under the engine mutex and returns before
	// SubmitAction does, so by now the events either sit in the buffered channel or
	// never will. A timeout would only make this slower and let a merely-slow
	// broadcast pass.
	//
	// The half-applied move must never reach a client, but the game really is over,
	// so the end of it has to be announced: without it every other player's view
	// waits on a frame that will never come and the lobby never records the match.
	var seen []EventType
	for {
		select {
		case ev := <-ch:
			seen = append(seen, ev.Type)
			continue
		default:
		}
		break
	}
	assert.Equal(t, []EventType{EventGameEnded}, seen,
		"a failed post-condition ends the game without publishing the action")
}

func TestEngine_RemovePlayer(t *testing.T) {
	t.Parallel()
	players := []*Player{{ID: "p1"}, {ID: "p2"}, {ID: "p3"}}
	m := setupMockRules()
	m.On("CheckWinCondition", mock.Anything).Return(false)
	engine := NewEngine(m, players, deck.StandardDeck())
	require.NoError(t, engine.Start())

	engine.RemovePlayer("p2")

	engine.WithState(func(state *State) {
		assert.Len(t, state.Players, 2)
		assert.Equal(t, Playing, state.Phase)
	})

	engine.RemovePlayer("p3")

	engine.WithState(func(state *State) {
		assert.Len(t, state.Players, 1)
		assert.Equal(t, Finished, state.Phase)
		assert.Equal(t, "p1", state.Winner.ID)
	})

	m.AssertExpectations(t)
}

// leaveAwareRules embeds MockRules and additionally implements
// game.PlayerLeaveHandler so tests can drive the mid-hand leave path. The two
// hooks delegate to configurable funcs so each test can inject behavior (e.g., a
// stale OverrideNextTurn) without touching MockRules' strict expectations.
type leaveAwareRules struct {
	*MockRules
	onLeave      func(state *State, playerID string)
	afterRemoved func(state *State, removedIndex int)
}

func (r *leaveAwareRules) OnPlayerLeave(state *State, playerID string) {
	if r.onLeave != nil {
		r.onLeave(state, playerID)
	}
}

func (r *leaveAwareRules) AfterPlayerRemoved(state *State, removedIndex int) {
	if r.afterRemoved != nil {
		r.afterRemoved(state, removedIndex)
	}
}

// TestEngine_RemovePlayer_MidTurnOverrideClamped is a regression test for a fixed CRITICAL
// Regression: a leave handler could set OverrideNextTurn to an index computed against
// the pre-removal seat count, which is past the end of the shortened Players slice.
func TestEngine_RemovePlayer_MidTurnOverrideClamped(t *testing.T) {
	t.Parallel()

	const preRemovalCount = 3 // seats before the leave; stale/out-of-range after.

	newEngine := func(t *testing.T) *Engine {
		t.Helper()
		players := []*Player{{ID: "p1"}, {ID: "p2"}, {ID: "p3"}}
		base := setupMockRules()
		base.On("CheckWinCondition", mock.Anything).Return(false)
		base.On("Standings", mock.Anything).Return([]*Player{}).Maybe()
		base.On("ValidateAction", mock.Anything, mock.Anything).Return(nil).Maybe()
		base.On("ApplyAction", mock.Anything, mock.Anything).Maybe()
		base.On("AfterAction", mock.Anything, mock.Anything).Return(nil).Maybe()

		r := &leaveAwareRules{MockRules: base}
		// Simulate a stale index computed before removal: the pre-removal player
		// count now points one seat past the end of the two remaining seats.
		r.afterRemoved = func(state *State, _ int) {
			state.OverrideNextTurn = new(preRemovalCount)
		}

		engine := NewEngine(r, players, deck.StandardDeck())
		require.NoError(t, engine.Start())
		return engine
	}

	seatedIDs := func(engine *Engine) map[string]bool {
		seated := make(map[string]bool)
		engine.WithState(func(state *State) {
			for _, p := range state.Players {
				seated[p.ID] = true
			}
		})
		return seated
	}

	// assertHealthyAfterRemove verifies the clamp + override-consume path: no
	// panic, a valid seated current player, and that this player can act.
	assertHealthyAfterRemove := func(t *testing.T, engine *Engine) {
		t.Helper()

		var currentID string
		require.NotPanics(t, func() { currentID = engine.CurrentPlayerID() })

		engine.WithState(func(state *State) {
			assert.Len(t, state.Players, 2)
			assert.GreaterOrEqual(t, state.CurrentTurn, 0)
			assert.Less(t, state.CurrentTurn, len(state.Players))
		})

		assert.True(t, seatedIDs(engine)[currentID],
			"current player %q must be a seated player", currentID)

		require.NotPanics(t, func() {
			err := engine.SubmitAction(currentID, MockAction{name: "Move"})
			assert.NoError(t, err)
		})
	}

	t.Run("non-current player leaves", func(t *testing.T) {
		t.Parallel()
		engine := newEngine(t)

		current := engine.CurrentPlayerID()
		victim := "p1"
		for _, id := range []string{"p1", "p2", "p3"} {
			if id != current {
				victim = id
				break
			}
		}
		require.NotEqual(t, current, victim)

		require.NotPanics(t, func() { engine.RemovePlayer(victim) })
		assertHealthyAfterRemove(t, engine)
	})

	t.Run("current player leaves", func(t *testing.T) {
		t.Parallel()
		engine := newEngine(t)

		current := engine.CurrentPlayerID()
		require.NotPanics(t, func() { engine.RemovePlayer(current) })
		assertHealthyAfterRemove(t, engine)
	})
}

// newStartedEngine is a two-seat game in the Playing phase with rules that accept
// anything, for tests that care about what the engine reports rather than the rules.
func newStartedEngine(t *testing.T, ids ...string) *Engine {
	t.Helper()
	players := make([]*Player, 0, len(ids))
	for _, id := range ids {
		players = append(players, &Player{ID: id})
	}

	m := setupMockRules()
	m.On("ValidateAction", mock.Anything, mock.Anything).Return(nil).Maybe()
	m.On("ApplyAction", mock.Anything, mock.Anything).Maybe()
	m.On("AfterAction", mock.Anything, mock.Anything).Return(nil).Maybe()
	m.On("CheckWinCondition", mock.Anything).Return(false).Maybe()
	m.On("Standings", mock.Anything).Return([]*Player{}).Maybe()

	engine := NewEngine(m, players, deck.StandardDeck())
	t.Cleanup(engine.Close)
	require.NoError(t, engine.Start())
	return engine
}

// Snapshot is the only thing the views render a table from, so every field it fills needs
// an assertion: a skipped one shows up as an empty table, not as a crash.
func TestEngine_Snapshot(t *testing.T) {
	t.Parallel()

	t.Run("public table state", func(t *testing.T) {
		t.Parallel()
		engine := newStartedEngine(t, "p1", "p2")

		snap := engine.Snapshot()

		assert.Equal(t, Playing, snap.Phase)
		assert.Equal(t, len(deck.StandardDeck())-2*5, snap.DeckSize, "what is left after dealing five each")
		assert.NotEmpty(t, snap.CurrentPlayer, "somebody is always on turn while playing")
		require.Len(t, snap.Players, 2, "every seat is listed")
		assert.Equal(t, "p1", snap.Players[0].ID)
		assert.Equal(t, 5, snap.Players[0].HandSize)
		assert.Empty(t, snap.Winner, "nobody has won yet")
	})

	t.Run("a finished game names the winner", func(t *testing.T) {
		t.Parallel()
		engine := newStartedEngine(t, "p1", "p2")
		engine.WithState(func(state *State) {
			state.Phase = Finished
			state.Winner = state.Players[1]
		})

		assert.Equal(t, "p2", engine.Snapshot().Winner)
	})

	// The cursor is clamped everywhere it is set, but Snapshot is reached from views
	// during a leave, so it guards the read as well. Both edges have to hold or the
	// guard is decoration.
	t.Run("cursor out of range names nobody", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name        string
			currentTurn int
			want        string
		}{
			{name: "first seat", currentTurn: 0, want: "p1"},
			{name: "last seat", currentTurn: 1, want: "p2"},
			{name: "one past the end", currentTurn: 2, want: ""},
			{name: "negative", currentTurn: -1, want: ""},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				engine := newStartedEngine(t, "p1", "p2")
				engine.WithState(func(state *State) { state.CurrentTurn = tt.currentTurn })

				var snap StateSnapshot
				require.NotPanics(t, func() { snap = engine.Snapshot() })
				assert.Equal(t, tt.want, snap.CurrentPlayer)
			})
		}
	})

	t.Run("a state without piles is still readable", func(t *testing.T) {
		t.Parallel()
		engine := newStartedEngine(t, "p1", "p2")
		engine.WithState(func(state *State) {
			state.Deck = nil
			state.Discard = nil
		})

		var snap StateSnapshot
		require.NotPanics(t, func() { snap = engine.Snapshot() })
		assert.Zero(t, snap.DeckSize)
		assert.Len(t, snap.Players, 2)
	})
}

// Standings feeds match persistence, so a player who left has to appear exactly once: twice
// inflates their placement, never means the lobby records the wrong result.
func TestEngine_Standings_PlacesPlayersWhoLeft(t *testing.T) {
	t.Parallel()

	t.Run("players the rules did not place are appended, latest leave first", func(t *testing.T) {
		t.Parallel()
		stayed := &Player{ID: "p1"}
		leftFirst := &Player{ID: "p2"}
		leftLast := &Player{ID: "p3"}

		m := setupMockRules()
		m.On("Standings", mock.Anything).Return([]*Player{stayed})
		engine := NewEngine(m, []*Player{stayed}, deck.StandardDeck())
		t.Cleanup(engine.Close)
		engine.WithState(func(state *State) {
			state.LeftPlayers = []*Player{leftFirst, leftLast}
		})

		assert.Equal(t, []string{"p1", "p3", "p2"}, engine.StandingsIDs())
	})

	t.Run("players the rules placed themselves are not repeated", func(t *testing.T) {
		t.Parallel()
		stayed := &Player{ID: "p1"}
		left := &Player{ID: "p2"}

		m := setupMockRules()
		// Poker ranks a departed player on the chips they walked out with, so the
		// engine must not append them a second time.
		m.On("Standings", mock.Anything).Return([]*Player{stayed, left})
		engine := NewEngine(m, []*Player{stayed}, deck.StandardDeck())
		t.Cleanup(engine.Close)
		engine.WithState(func(state *State) { state.LeftPlayers = []*Player{left} })

		assert.Equal(t, []string{"p1", "p2"}, engine.StandingsIDs())
	})
}

// An ID that is not seated must match no seat at all.
func TestEngine_RemovePlayer_UnknownIDIsANoOp(t *testing.T) {
	t.Parallel()
	engine := newStartedEngine(t, "p1", "p2", "p3")

	before := engine.CurrentPlayerID()
	engine.RemovePlayer("nobody")

	engine.WithState(func(state *State) {
		assert.Len(t, state.Players, 3, "no seat belongs to an unknown ID")
		assert.Empty(t, state.LeftPlayers)
	})
	assert.Equal(t, before, engine.CurrentPlayerID(), "and the turn does not move")
}

// The end of a game has to say who won: the lobby persists the match from this event, and
// every client is waiting on it to leave the table.
func TestEngine_GameEndedNamesTheWinner(t *testing.T) {
	t.Parallel()
	winner := &Player{ID: "p2"}
	loser := &Player{ID: "p1"}

	m := setupMockRules()
	action := MockAction{name: "Win"}
	m.On("ValidateAction", mock.Anything, action).Return(nil)
	m.On("ApplyAction", mock.Anything, action)
	m.On("AfterAction", mock.Anything, action).Return(nil)
	m.On("CheckWinCondition", mock.Anything).Return(true)
	m.On("Standings", mock.Anything).Return([]*Player{winner, loser})

	engine := NewEngine(m, []*Player{loser, winner}, deck.StandardDeck())
	t.Cleanup(engine.Close)
	events, err := engine.Broadcaster().Subscribe()
	require.NoError(t, err)
	require.NoError(t, engine.Start())

	require.NoError(t, engine.SubmitAction(engine.CurrentPlayerID(), action))

	var ended []Event
	for {
		select {
		case ev := <-events:
			if ev.Type == EventGameEnded {
				ended = append(ended, ev)
			}
			continue
		default:
		}
		break
	}
	require.Len(t, ended, 1)
	assert.Equal(t, "p2", ended[0].PlayerID, "the winner has to be named on the wire")
}

// The subscriber cap is the player count plus headroom for the lobby's ranked-finalize
// watcher and reconnect overlap.
func TestEngine_SubscriberHeadroomAbovePlayerCount(t *testing.T) {
	t.Parallel()
	players := []*Player{{ID: "p1"}, {ID: "p2"}}
	engine := NewEngine(setupMockRules(), players, deck.StandardDeck())
	t.Cleanup(engine.Close)

	for i := range len(players) + 8 {
		_, err := engine.Broadcaster().Subscribe()
		require.NoErrorf(t, err, "subscriber %d must fit", i)
	}

	_, err := engine.Broadcaster().Subscribe()
	require.ErrorIs(t, err, broadcaster.ErrAtCapacity, "the cap is players plus headroom, not unbounded")
}

// Close has to close the broadcaster, not just the clock: a subscriber that is never told
// the engine is gone parks a goroutine for the life of the process.
func TestEngine_CloseClosesTheBroadcaster(t *testing.T) {
	t.Parallel()
	engine := newStartedEngine(t, "p1", "p2")

	engine.Close()

	_, err := engine.Broadcaster().Subscribe()
	assert.ErrorIs(t, err, broadcaster.ErrClosed)
}

func TestCryptoIntN(t *testing.T) {
	t.Parallel()

	// An empty table must not reach crypto/rand, which panics on a non-positive bound.
	for _, n := range []int{0, -1} {
		_, err := cryptoIntN(n)
		require.Errorf(t, err, "cryptoIntN(%d) must refuse rather than panic", n)
	}

	v, err := cryptoIntN(1)
	require.NoError(t, err)
	assert.Zero(t, v, "the only seat available")
}

// TestEngine_ConcurrentOperations hammers the engine from many goroutines to prove there is
// no data race or panic and that the engine finishes in a valid, consistent state.
func TestEngine_ConcurrentOperations(t *testing.T) {
	t.Parallel()

	const playerCount = 8
	players := make([]*Player, playerCount)
	ids := make([]string, playerCount)
	for i := range players {
		id := fmt.Sprintf("p%d", i)
		players[i] = &Player{ID: id}
		ids[i] = id
	}

	base := setupMockRules()
	base.On("ValidateAction", mock.Anything, mock.Anything).Return(nil).Maybe()
	base.On("ApplyAction", mock.Anything, mock.Anything).Maybe()
	base.On("AfterAction", mock.Anything, mock.Anything).Return(nil).Maybe()
	base.On("CheckWinCondition", mock.Anything).Return(false).Maybe()
	base.On("Standings", mock.Anything).Return(players).Maybe()

	engine := NewEngine(base, players, deck.StandardDeck())
	require.NoError(t, engine.Start())

	var wg sync.WaitGroup

	// Action submitters: every seat repeatedly tries to act. Most calls lose the
	// turn race and error out; that is expected and must never panic.
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			for range 50 {
				_ = engine.SubmitAction(id, MockAction{name: "Move"})
			}
		}(id)
	}

	// Readers: concurrent snapshot / standings / current-player queries.
	for range 4 {
		wg.Go(func() {
			for range 200 {
				_ = engine.CurrentPlayerID()
				_ = engine.Snapshot()
				_ = engine.Standings()
				_ = engine.StandingsIDs()
			}
		})
	}

	// Removers: drop a subset of seats concurrently, always leaving at least two
	// so the game never collapses to a trivial single-player finish here.
	for _, id := range ids[:playerCount-2] {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			engine.RemovePlayer(id)
		}(id)
	}

	wg.Wait()

	// The engine must remain internally consistent regardless of interleaving.
	engine.WithState(func(state *State) {
		assert.GreaterOrEqual(t, len(state.Players), 1)
		assert.True(t, state.Phase == Playing || state.Phase == Finished,
			"unexpected phase %v", state.Phase)
		assert.GreaterOrEqual(t, state.CurrentTurn, 0)
		assert.Less(t, state.CurrentTurn, len(state.Players))
	})
	require.NotPanics(t, func() { _ = engine.CurrentPlayerID() })
}
