package lobby

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// newFinishedGameLobby is a two-seat ranked lobby whose game ends the moment both
// players are ready, with the watcher goroutine running.
func newFinishedGameLobby(t *testing.T, repo db.MatchRepository) (*Manager, *Lobby, *game.Engine) {
	t.Helper()
	m := NewManager(context.Background(), repo)
	leader := mockPlayer("leader", 1)
	guest := mockPlayer("guest", 2)

	l, err := m.New(leader, WithMaxPlayers(2), WithCardGame("MockGame"), WithRanked(true))
	require.NoError(t, err)
	require.NoError(t, m.JoinLobbyByCode(l.Code(), guest))

	registry := game.NewRegistry()
	rules := new(MockRules)
	rules.On("MinPlayers").Return(2).Maybe()
	rules.On("MaxPlayers").Return(4).Maybe()
	rules.On("InitialDeck").Return(deck.StandardDeck()).Maybe()
	rules.On("InitialDealCount").Return(2).Maybe()
	rules.On("OnGameStart", mock.Anything).Return(nil).Maybe()
	rules.On("CheckWinCondition", mock.Anything).Return(false).Maybe()
	rules.On("Standings", mock.Anything).Return([]*game.Player{leader, guest}).Maybe()
	registerGame(registry, "MockGame", rules)

	require.NoError(t, l.ToggleReady(leader, registry))
	require.NoError(t, l.ToggleReady(guest, registry))

	l.mu.RLock()
	engine := l.activeEngine
	l.mu.RUnlock()
	require.NotNil(t, engine)
	return m, l, engine
}

// A match that ends while the lobby is being torn down must not be recorded twice,
// and the watcher goroutine must not outlive either path.
func TestConcurrent_FinalizeRacesRemoveLobby(t *testing.T) {
	t.Parallel()

	repo := new(MockMatchRepo)
	recorded := make(chan struct{}, 4)
	repo.On("FinalizeRankedMatch", mock.Anything, "MockGame", []uint{1, 2}, mock.Anything).
		Run(func(mock.Arguments) { recorded <- struct{}{} }).
		Return(nil)

	m, l, engine := newFinishedGameLobby(t, repo)
	// The hand really is over, so both racers have a result to persist: the watcher
	// through the event, or - if RemoveLobby closes the feed first and the event is
	// never delivered - through the finished engine it finds when the feed ends.
	engine.WithState(func(state *game.State) { state.Phase = game.Finished })

	go engine.Broadcaster().Broadcast(game.Event{Type: game.EventGameEnded, Reason: game.EndReasonWin})
	go m.RemoveLobby(l.Code())

	select {
	case <-recorded:
	case <-time.After(2 * time.Second):
		t.Fatal("the finished match was never recorded")
	}
	// Whichever path lost the race must not write a second row for the same hand.
	require.True(t, m.WaitForFinalizers(2*time.Second), "a finalizer outlived the drain")
	assert.Empty(t, recorded, "the match was recorded more than once")
}

// A single hand produces a single row no matter how many times the terminal event is
// published: the engine ends once, the watcher stops reading after the first.
func TestFinalize_IsNotAppliedTwice(t *testing.T) {
	t.Parallel()

	repo := new(MockMatchRepo)
	calls := make(chan struct{}, 4)
	repo.On("FinalizeRankedMatch", mock.Anything, "MockGame", []uint{1, 2}, mock.Anything).
		Run(func(mock.Arguments) { calls <- struct{}{} }).
		Return(nil)

	m, _, engine := newFinishedGameLobby(t, repo)

	engine.Broadcaster().Broadcast(game.Event{Type: game.EventGameEnded, Reason: game.EndReasonWin})
	engine.Broadcaster().Broadcast(game.Event{Type: game.EventGameEnded, Reason: game.EndReasonWin})

	select {
	case <-calls:
	case <-time.After(2 * time.Second):
		t.Fatal("the finished match was never recorded")
	}
	require.True(t, m.WaitForFinalizers(2*time.Second))
	assert.Empty(t, calls, "the same hand was persisted twice")
}

// The window between observing the end of a game and registering the finalizer used
// to be a silent drop: shutdown could start inside it and the result vanished with
// nothing logged and nothing for WaitForFinalizers to wait on.
//
//nolint:paralleltest // slog.SetDefault is process-wide, so this cannot share the process
func TestShutdown_MatchEndingDuringDrainIsNotSilentlyDropped(t *testing.T) {
	logged := &syncBuffer{}
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logged, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { slog.SetDefault(original) })

	for i := range 20 {
		var recorded atomic.Int32
		repo := new(MockMatchRepo)
		repo.On("FinalizeRankedMatch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Run(func(mock.Arguments) { recorded.Add(1) }).Return(nil)
		repo.On("RecordCasualMatch", mock.Anything, mock.Anything, mock.Anything).
			Run(func(mock.Arguments) { recorded.Add(1) }).Return(nil)

		m, l, engine := newFinishedGameLobby(t, repo)
		logged.Reset()

		done := make(chan struct{})
		go func() {
			defer close(done)
			engine.Broadcaster().Broadcast(game.Event{Type: game.EventGameEnded, Reason: game.EndReasonWin})
		}()
		drained := m.WaitForFinalizers(2 * time.Second)
		<-done
		require.True(t, drained, "the drain gave up on iteration %d", i)

		// Every iteration either persisted the result or shouted that it could not.
		require.Eventually(t, func() bool {
			return recorded.Load() > 0 || logged.contains("finished match dropped")
		}, 2*time.Second, 5*time.Millisecond,
			"iteration %d neither persisted the match nor logged the drop", i)

		m.RemoveLobby(l.Code())
	}
}

// syncBuffer is a log sink two goroutines can touch: the finalizer writes to it while
// the assertion polls it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p) //nolint:wrapcheck // io.Writer contract
}

func (b *syncBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}

func (b *syncBuffer) contains(want string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Contains(b.buf.Bytes(), []byte(want))
}

// A leader who can kick mid-hand can farm Elo: drop whoever is winning and let the
// engine hand them the rating.
func TestKick_IsRejectedWhileInGame(t *testing.T) {
	t.Parallel()

	m, l, registry := newTestLobby(t, 2)
	leader := l.Leader()
	guest := mockPlayer("p2", 2)
	require.NoError(t, m.JoinLobbyByCode(l.Code(), guest))

	require.NoError(t, l.ToggleReady(leader, registry))
	require.NoError(t, l.ToggleReady(guest, registry))
	require.Equal(t, InGame, l.state)

	require.ErrorContains(t, m.Kick(leader, guest), "cannot kick during a game")
	assert.True(t, l.HasPlayer(guest), "the target is still at the table")
	assert.Equal(t, l, m.FindLobbyByPlayer(guest), "and still indexed to it")

	l.mu.RLock()
	engine := l.activeEngine
	l.mu.RUnlock()
	engine.WithState(func(state *game.State) { state.Phase = game.Finished })
	l.releaseFinishedGame()

	require.NoError(t, m.Kick(leader, guest), "and can be kicked once the hand is over")
}

// The ready flip is committed before the start is attempted, so a failed start still
// has to publish it; otherwise every other client shows a stale roster.
func TestToggleReady_FailedStartStillBroadcasts(t *testing.T) {
	t.Parallel()

	m := NewManager(context.Background(), nil)
	leader := mockPlayer("p1", 1)
	guest := mockPlayer("p2", 2)
	l, err := m.New(leader, WithMaxPlayers(4), WithCardGame("Unregistered"))
	require.NoError(t, err)
	require.NoError(t, m.JoinLobbyByCode(l.Code(), guest))

	observer, err := l.Subscribe("observer")
	require.NoError(t, err)

	// The registry has no rules under this name, so the start fails after both flips.
	require.NoError(t, l.ToggleReady(leader, game.NewRegistry()))
	require.Error(t, l.ToggleReady(guest, game.NewRegistry()))

	assert.Equal(t, []string{EventPlayersUpdated, EventPlayersUpdated}, drainEventTypes(observer),
		"the second flip was committed without telling anyone")
	assert.True(t, l.IsReady(guest), "and it really was committed")
}

// A drain that times out used to strand its waiter goroutine, one per attempt, on a
// path that is retried by design.
func TestWaitForFinalizers_TimeoutDoesNotLeakItsWaiter(t *testing.T) {
	t.Parallel()

	m := NewManager(context.Background(), nil)
	require.True(t, m.registerFinalizer())

	assert.False(t, m.WaitForFinalizers(10*time.Millisecond), "an in-flight write blocks the drain")

	m.finalizerMu.Lock()
	first := m.drained
	m.finalizerMu.Unlock()

	assert.False(t, m.WaitForFinalizers(10*time.Millisecond))

	m.finalizerMu.Lock()
	second := m.drained
	m.finalizerMu.Unlock()
	assert.Equal(t, first, second, "a second attempt started a second waiter")

	// goleak's TestMain is the other half of this: the one waiter has to exit.
	m.finalizing.Done()
	assert.True(t, m.WaitForFinalizers(2*time.Second))
}

// A ranked hand the deploy interrupted has no honest winner, so it is history only.
func TestFinalize_InterruptedRankedMatchIsRecordedWithoutElo(t *testing.T) {
	t.Parallel()

	repo := new(MockMatchRepo)
	recorded := make(chan struct{}, 1)
	repo.On("RecordCasualMatch", mock.Anything, "MockGame", []uint{1, 2}).
		Run(func(mock.Arguments) { recorded <- struct{}{} }).
		Return(nil)

	m, _, engine := newFinishedGameLobby(t, repo)
	m.BeginShutdown()

	engine.Broadcaster().Broadcast(game.Event{Type: game.EventGameEnded, Reason: game.EndReasonForfeit})

	select {
	case <-recorded:
	case <-time.After(2 * time.Second):
		t.Fatal("the interrupted match was never recorded")
	}
	require.True(t, m.WaitForFinalizers(2*time.Second))
	repo.AssertNotCalled(t, "FinalizeRankedMatch",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// A rules bug that ends the hand must not move the ladder: half-applied state is not a result.
func TestFinalize_RulesErrorIsRecordedWithoutElo(t *testing.T) {
	t.Parallel()

	repo := new(MockMatchRepo)
	recorded := make(chan struct{}, 1)
	repo.On("RecordCasualMatch", mock.Anything, "MockGame", []uint{1, 2}).
		Run(func(mock.Arguments) { recorded <- struct{}{} }).
		Return(nil)

	_, _, engine := newFinishedGameLobby(t, repo)
	engine.Broadcaster().Broadcast(game.Event{Type: game.EventGameEnded, Reason: game.EndReasonRulesError})

	select {
	case <-recorded:
	case <-time.After(2 * time.Second):
		t.Fatal("the rules-error match was never recorded")
	}
	repo.AssertNotCalled(t, "FinalizeRankedMatch",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// A dropped session must not forfeit a live match: the seat survives for the grace
// window and a reconnect cancels the pending leave.
func TestDisconnectPlayer_MidGameSeatSurvivesTheGraceWindow(t *testing.T) {
	t.Parallel()

	m, l, registry := newTestLobby(t, 2)
	leader := l.Leader()
	guest := mockPlayer("p2", 2)
	require.NoError(t, m.JoinLobbyByCode(l.Code(), guest))
	require.NoError(t, l.ToggleReady(leader, registry))
	require.NoError(t, l.ToggleReady(guest, registry))
	require.Equal(t, InGame, l.state)

	m.DisconnectPlayer(guest)

	assert.True(t, l.HasPlayer(guest), "the seat is held for the grace window")
	assert.Equal(t, l, m.FindLobbyByPlayer(guest), "and stays indexed")

	resumed := m.ResumePlayer(guest)
	require.Equal(t, l, resumed, "a reconnect lands back at the same lobby")
	m.mu.Lock()
	_, pending := m.grace.pending[guest.ID]
	m.mu.Unlock()
	assert.False(t, pending, "the pending leave is cancelled by the resume")
}

func TestDisconnectPlayer_GraceExpiryForfeitsTheSeat(t *testing.T) {
	t.Parallel()

	m, l, registry := newTestLobby(t, 2)
	leader := l.Leader()
	guest := mockPlayer("p2", 2)
	require.NoError(t, m.JoinLobbyByCode(l.Code(), guest))
	require.NoError(t, l.ToggleReady(leader, registry))
	require.NoError(t, l.ToggleReady(guest, registry))

	m.DisconnectPlayer(guest)
	m.expireLeave(guest)

	assert.False(t, l.HasPlayer(guest), "the expired seat is given up")
	assert.Nil(t, m.FindLobbyByPlayer(guest))
	assert.Nil(t, m.ResumePlayer(guest), "nothing to resume after expiry")
}

// expireLeave deletes the pending entry before LeaveLobby; ResumePlayer must not
// treat a missing entry as "still seated" or it routes into a seat about to vanish.
func TestResumePlayer_AfterExpireClaimReturnsNil(t *testing.T) {
	t.Parallel()

	m, l, registry := newTestLobby(t, 2)
	leader := l.Leader()
	guest := mockPlayer("p2", 2)
	require.NoError(t, m.JoinLobbyByCode(l.Code(), guest))
	require.NoError(t, l.ToggleReady(leader, registry))
	require.NoError(t, l.ToggleReady(guest, registry))

	m.DisconnectPlayer(guest)
	m.mu.Lock()
	delete(m.grace.pending, guest.ID) // what expireLeave does before LeaveLobby
	m.grace.expiring[guest.ID] = struct{}{}
	stillSeated := m.playerLobby[guest.ID] != nil
	m.mu.Unlock()
	require.True(t, stillSeated)

	assert.Nil(t, m.ResumePlayer(guest), "grace already claimed: do not resume")
	assert.True(t, l.HasPlayer(guest), "LeaveLobby has not run yet")
}

// A second SSH session can take over a mid-game seat while the first is still
// half-open (no pending leave yet).
func TestResumePlayer_TakeoverWithoutPendingLeave(t *testing.T) {
	t.Parallel()

	m, l, registry := newTestLobby(t, 2)
	leader := l.Leader()
	guest := mockPlayer("p2", 2)
	require.NoError(t, m.JoinLobbyByCode(l.Code(), guest))
	require.NoError(t, l.ToggleReady(leader, registry))
	require.NoError(t, l.ToggleReady(guest, registry))

	assert.Equal(t, l, m.ResumePlayer(guest), "zombie session still holds the seat")
}

func TestDisconnectPlayer_WaitingLobbyLeavesImmediately(t *testing.T) {
	t.Parallel()

	m, l, _ := newTestLobby(t, 2)
	guest := mockPlayer("p2", 2)
	require.NoError(t, m.JoinLobbyByCode(l.Code(), guest))
	require.Equal(t, Waiting, l.state)

	m.DisconnectPlayer(guest)

	assert.False(t, l.HasPlayer(guest), "nothing is lost by leaving a waiting lobby")
	assert.Nil(t, m.FindLobbyByPlayer(guest))
}
