package game

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/player"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockRules struct {
	mock.Mock
}

func (m *MockRules) Name() string {
	args := m.Called()
	return args.String(0)
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

func (m *MockRules) PreActionCondition(state *State, action Action) error {
	args := m.Called(state, action)
	return args.Error(0)
}

func (m *MockRules) ApplyAction(state *State, action Action) {
	m.Called(state, action)
}

func (m *MockRules) PostActionCondition(state *State, action Action) error {
	args := m.Called(state, action)
	return args.Error(0)
}

func (m *MockRules) CheckWinCondition(state *State) bool {
	args := m.Called(state)
	return args.Bool(0)
}

func (m *MockRules) GetStandings(state *State) []*player.Player {
	args := m.Called(state)
	return args.Get(0).([]*player.Player)
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
	players := []*player.Player{{ID: "p1"}, {ID: "p2"}}
	m := setupMockRules()
	engine := NewEngine(m, players, deck.StandardDeck())

	err := engine.Start()
	assert.NoError(t, err)

	engine.WithState(func(state *State) {
		assert.Equal(t, Playing, state.Phase)
		assert.Len(t, state.Players[0].Cards, 5)
		assert.Len(t, state.Players[1].Cards, 5)
	})

	m.AssertExpectations(t)
}

type MockAction struct {
	name string
}

func (m MockAction) Name() string {
	return m.name
}

func TestEngine_SubmitAction(t *testing.T) {
	t.Parallel()
	players := []*player.Player{{ID: "p1"}, {ID: "p2"}}
	m := setupMockRules()
	engine := NewEngine(m, players, deck.StandardDeck())
	engine.Start()

	currentPlayerID := engine.CurrentPlayerID()
	otherPlayerID := "p2"
	if currentPlayerID == "p2" {
		otherPlayerID = "p1"
	}

	err := engine.SubmitAction(otherPlayerID, MockAction{name: "MockDraw"})
	assert.ErrorContains(t, err, "wait for your turn")

	validAction := MockAction{name: "MockDraw"}
	m.On("PreActionCondition", mock.Anything, validAction).Return(nil)
	m.On("ApplyAction", mock.Anything, validAction)
	m.On("PostActionCondition", mock.Anything, validAction).Return(nil)
	m.On("CheckWinCondition", mock.Anything).Return(false)

	err = engine.SubmitAction(currentPlayerID, validAction)
	assert.NoError(t, err)

	assert.Equal(t, otherPlayerID, engine.CurrentPlayerID())

	m.AssertExpectations(t)
}

func TestEngine_SubmitAction_SetsWinnerFromStandings(t *testing.T) {
	t.Parallel()
	winner := &player.Player{ID: "p2"}
	loser := &player.Player{ID: "p1"}
	players := []*player.Player{loser, winner}

	m := setupMockRules()
	engine := NewEngine(m, players, deck.StandardDeck())
	require.NoError(t, engine.Start())

	currentPlayerID := engine.CurrentPlayerID()
	action := MockAction{name: "Win"}
	m.On("PreActionCondition", mock.Anything, action).Return(nil)
	m.On("ApplyAction", mock.Anything, action)
	m.On("PostActionCondition", mock.Anything, action).Return(nil)
	m.On("CheckWinCondition", mock.Anything).Return(true)
	m.On("GetStandings", mock.Anything).Return([]*player.Player{winner, loser})

	err := engine.SubmitAction(currentPlayerID, action)
	require.NoError(t, err)

	engine.WithState(func(state *State) {
		assert.Equal(t, Finished, state.Phase)
		assert.Equal(t, "p2", state.Winner.ID)
	})
}

func TestEngine_SubmitAction_PostConditionBeforeBroadcast(t *testing.T) {
	t.Parallel()
	players := []*player.Player{{ID: "p1"}, {ID: "p2"}}
	m := setupMockRules()
	engine := NewEngine(m, players, deck.StandardDeck())

	ch := engine.Broadcaster().Subscribe()
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
	m.On("PreActionCondition", mock.Anything, action).Return(nil)
	m.On("ApplyAction", mock.Anything, action)
	m.On("PostActionCondition", mock.Anything, action).Return(assert.AnError)

	err := engine.SubmitAction(currentPlayerID, action)
	require.Error(t, err)

	engine.WithState(func(state *State) {
		assert.Equal(t, Finished, state.Phase)
	})

	select {
	case ev := <-ch:
		t.Fatalf("unexpected broadcast after post-condition failure: %+v", ev)
	case <-time.After(50 * time.Millisecond):
		// expected: no EventActionApplied
	}
}

func TestEngine_RemovePlayer(t *testing.T) {
	t.Parallel()
	players := []*player.Player{{ID: "p1"}, {ID: "p2"}, {ID: "p3"}}
	m := setupMockRules()
	m.On("CheckWinCondition", mock.Anything).Return(false)
	engine := NewEngine(m, players, deck.StandardDeck())
	engine.Start()

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
// hooks delegate to configurable funcs so each test can inject behavior (e.g. a
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

// TestEngine_RemovePlayer_MidTurnOverrideClamped is a regression test for a
// fixed CRITICAL bug: when a player left mid-hand, the leave handler could set
// state.OverrideNextTurn to an index computed against the pre-removal seat count
// that was now past the end of the shortened Players slice. The engine must
// consume that override and clamp it into range so the next turn lookup and
// SubmitAction can never index out of range.
func TestEngine_RemovePlayer_MidTurnOverrideClamped(t *testing.T) {
	t.Parallel()

	const preRemovalCount = 3 // seats before the leave; stale/out-of-range after.

	newEngine := func(t *testing.T) *Engine {
		t.Helper()
		players := []*player.Player{{ID: "p1"}, {ID: "p2"}, {ID: "p3"}}
		base := setupMockRules()
		base.On("CheckWinCondition", mock.Anything).Return(false)
		base.On("GetStandings", mock.Anything).Return([]*player.Player{}).Maybe()
		base.On("PreActionCondition", mock.Anything, mock.Anything).Return(nil).Maybe()
		base.On("ApplyAction", mock.Anything, mock.Anything).Maybe()
		base.On("PostActionCondition", mock.Anything, mock.Anything).Return(nil).Maybe()

		r := &leaveAwareRules{MockRules: base}
		// Simulate a stale index computed before removal: the pre-removal player
		// count now points one seat past the end of the two remaining seats.
		r.afterRemoved = func(state *State, _ int) {
			stale := preRemovalCount
			state.OverrideNextTurn = &stale
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

// TestEngine_ConcurrentOperations hammers the engine from many goroutines to
// prove there is no data race or panic and that the engine finishes in a valid,
// consistent state. Meaningful only under `go test -race`.
func TestEngine_ConcurrentOperations(t *testing.T) {
	t.Parallel()

	const playerCount = 8
	players := make([]*player.Player, playerCount)
	ids := make([]string, playerCount)
	for i := range players {
		id := fmt.Sprintf("p%d", i)
		players[i] = &player.Player{ID: id}
		ids[i] = id
	}

	base := setupMockRules()
	base.On("PreActionCondition", mock.Anything, mock.Anything).Return(nil).Maybe()
	base.On("ApplyAction", mock.Anything, mock.Anything).Maybe()
	base.On("PostActionCondition", mock.Anything, mock.Anything).Return(nil).Maybe()
	base.On("CheckWinCondition", mock.Anything).Return(false).Maybe()
	base.On("GetStandings", mock.Anything).Return(players).Maybe()

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
				_ = engine.SnapshotFor("p0")
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
