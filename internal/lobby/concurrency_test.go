package lobby

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/player"
	"github.com/Pieczasz/terminal-card/internal/ratelimit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unlimitedJoins swaps the per-player join rate limiter for one that never
// throttles, so a fan-out of concurrent joins is not masked by rate limiting.
func unlimitedJoins(m *Manager) {
	m.joinLimiter = ratelimit.NewSlidingWindowLimiter(1_000_000, time.Hour)
}

// runWithTimeout runs fn in a goroutine and fails the test loudly if it does not
// finish within d, so a deadlock in the subsystem fails instead of hanging the
// whole `go test` run forever.
func runWithTimeout(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("timed out after %s waiting for concurrent work to finish (possible deadlock)", d)
	}
}

// guestRef pairs a joiner with the per-goroutine outcome of its join attempt.
type guestRef struct {
	p      *player.Player
	joined bool
}

// TestConcurrent_JoinUpToCapacity fans out many simultaneous joins against a
// single lobby and asserts capacity is respected exactly: no over-join, no
// double-join, and every rejected joiner gets a "full" error.
func TestConcurrent_JoinUpToCapacity(t *testing.T) {
	t.Parallel()

	const (
		maxPlayers = 6  // leader + 5 guest slots
		joiners    = 64 // far more than free slots
	)
	freeSlots := maxPlayers - 1 // leader already occupies one slot

	m := NewManager(nil)
	unlimitedJoins(m)
	leader := mockPlayer("leader", 1)
	l, err := m.New(leader, WithMaxPlayers(maxPlayers), WithCardGame(&db.Game{Name: "TestGame"}))
	require.NoError(t, err)

	guests := make([]*guestRef, joiners)
	for i := range guests {
		guests[i] = &guestRef{p: mockPlayer(fmt.Sprintf("g%d", i), uint(i+2))}
	}

	var (
		successes atomic.Int64
		fullErrs  atomic.Int64
		otherErrs atomic.Int64
		start     = make(chan struct{})
		wg        sync.WaitGroup
	)

	for _, g := range guests {
		wg.Add(1)
		go func(g *guestRef) {
			defer wg.Done()
			<-start // release all goroutines together to maximise contention
			err := m.JoinLobbyByCode(l.Code(), g.p)
			switch {
			case err == nil:
				g.joined = true
				successes.Add(1)
			case strings.Contains(err.Error(), "full"):
				fullErrs.Add(1)
			default:
				otherErrs.Add(1)
			}
		}(g)
	}

	runWithTimeout(t, 10*time.Second, func() {
		close(start)
		wg.Wait()
	})

	assert.Zero(t, otherErrs.Load(), "unexpected non-full errors from rejected joiners")
	assert.Equal(t, int64(freeSlots), successes.Load(), "exactly the free slots should be filled")
	assert.Equal(t, int64(joiners-freeSlots), fullErrs.Load(), "all remaining joiners must be rejected as full")

	// Roster is exactly full and contains no duplicates.
	assert.Equal(t, maxPlayers, l.CurrentPlayers(), "lobby must end exactly at capacity")

	seen := map[string]bool{leader.ID: true}
	joinedCount := 0
	for _, g := range guests {
		if g.joined {
			joinedCount++
			require.True(t, l.HasPlayer(g.p), "successful joiner must be a member")
			require.False(t, seen[g.p.ID], "player joined more than once (double-join)")
			seen[g.p.ID] = true
			require.Equal(t, l, m.FindLobbyByPlayer(g.p), "playerLobby mapping must point at the lobby")
		} else {
			assert.False(t, l.HasPlayer(g.p), "rejected joiner must not be a member")
			assert.Nil(t, m.FindLobbyByPlayer(g.p), "rejected joiner must not have a playerLobby entry")
		}
	}
	assert.Equal(t, freeSlots, joinedCount)
}

// TestConcurrent_LeaderAndGuestsLeaveSimultaneously drains a full lobby by having
// the leader and every guest call LeaveLobby at the same time. The lobby must end
// up removed with no ghost membership left in the manager, and nothing may panic.
func TestConcurrent_LeaderAndGuestsLeaveSimultaneously(t *testing.T) {
	t.Parallel()

	const players = 8 // leader + 7 guests

	m := NewManager(nil)
	unlimitedJoins(m)
	leader := mockPlayer("leader", 1)
	l, err := m.New(leader, WithMaxPlayers(players), WithCardGame(&db.Game{Name: "TestGame"}))
	require.NoError(t, err)

	all := []*player.Player{leader}
	for i := 1; i < players; i++ {
		g := mockPlayer(fmt.Sprintf("g%d", i), uint(i+1))
		require.NoError(t, m.JoinLobbyByCode(l.Code(), g))
		all = append(all, g)
	}
	require.Equal(t, players, l.CurrentPlayers())

	var (
		start = make(chan struct{})
		wg    sync.WaitGroup
	)
	for _, p := range all {
		wg.Add(1)
		go func(p *player.Player) {
			defer wg.Done()
			<-start
			m.LeaveLobby(p)
		}(p)
	}

	runWithTimeout(t, 10*time.Second, func() {
		close(start)
		wg.Wait()
	})

	// Lobby must be gone from the manager entirely.
	_, err = m.FindLobbyByCode(l.Code())
	assert.ErrorContains(t, err, "lobby not found", "empty lobby must be removed")

	// No ghost membership: the manager's playerLobby map is the authoritative
	// membership index, and no player may still resolve to a lobby through it.
	// (The removed lobby object's own roster slice is intentionally left as-is by
	// RemoveLobby since the object is discarded, so it is not a membership signal.)
	for _, p := range all {
		assert.Nil(t, m.FindLobbyByPlayer(p), "player %q left a ghost playerLobby entry", p.ID)
	}
}

// TestConcurrent_JoinRacingLastLeave repeatedly interleaves a fresh joiner with
// the sole occupant (leader) leaving. Whatever the interleaving, the manager must
// land in one of two consistent states: the lobby was removed and the joiner is
// not seated, or the joiner is properly seated in a live lobby. No orphan entries
// either way.
func TestConcurrent_JoinRacingLastLeave(t *testing.T) {
	t.Parallel()

	const iterations = 500

	runWithTimeout(t, 30*time.Second, func() {
		for i := range iterations {
			m := NewManager(nil)
			unlimitedJoins(m)

			leader := mockPlayer(fmt.Sprintf("leader-%d", i), uint(2*i+1))
			l, err := m.New(leader, WithMaxPlayers(4), WithCardGame(&db.Game{Name: "TestGame"}))
			require.NoError(t, err)
			code := l.Code()

			joiner := mockPlayer(fmt.Sprintf("joiner-%d", i), uint(2*i+2))

			var (
				wg      sync.WaitGroup
				gate    = make(chan struct{})
				joinErr error
			)
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-gate
				joinErr = m.JoinLobbyByCode(code, joiner)
			}()
			go func() {
				defer wg.Done()
				<-gate
				m.LeaveLobby(leader)
			}()
			close(gate)
			wg.Wait()

			// Leader always left, so it must never be a lingering member.
			assert.Nil(t, m.FindLobbyByPlayer(leader), "iter %d: leader left a ghost entry", i)

			_, findErr := m.FindLobbyByCode(code)
			lobbyGone := findErr != nil

			if joinErr == nil {
				// Joiner was seated: the lobby must still exist and hold them.
				require.False(t, lobbyGone, "iter %d: join succeeded but lobby is gone", i)
				assert.True(t, l.HasPlayer(joiner), "iter %d: seated joiner missing from roster", i)
				assert.Equal(t, l, m.FindLobbyByPlayer(joiner), "iter %d: seated joiner missing mapping", i)
			} else {
				// Joiner was rejected: it must own no membership anywhere.
				assert.Nil(t, m.FindLobbyByPlayer(joiner), "iter %d: rejected joiner left a ghost entry", i)
				assert.False(t, l.HasPlayer(joiner), "iter %d: rejected joiner still a member", i)
			}
		}
	})
}

// TestConcurrent_ToggleReady hammers ToggleReady from many goroutines across all
// members of one lobby. Game rules demand more players than are present so a start
// never fires, keeping the lobby in Waiting while the ready map is mutated under
// contention. The point is race-detector cleanliness plus a consistent end state.
func TestConcurrent_ToggleReady(t *testing.T) {
	t.Parallel()

	const (
		members    = 5
		togglers   = 32
		perGoTurns = 40
	)

	m := NewManager(nil)
	unlimitedJoins(m)
	leader := mockPlayer("leader", 1)
	l, err := m.New(leader, WithMaxPlayers(members), WithCardGame(&db.Game{Name: "NeverStarts"}))
	require.NoError(t, err)

	roster := []*player.Player{leader}
	for i := 1; i < members; i++ {
		g := mockPlayer(fmt.Sprintf("g%d", i), uint(i+1))
		require.NoError(t, m.JoinLobbyByCode(l.Code(), g))
		roster = append(roster, g)
	}

	// Rules requiring far more players than present: an all-ready roster fails to
	// start, so the lobby stays in Waiting and the ready map keeps churning.
	registry := game.NewRegistry()
	mockRules := new(MockRules)
	mockRules.On("MinPlayers").Return(members + 100)
	registry.Register("NeverStarts", func() game.Rules { return mockRules })

	var (
		start = make(chan struct{})
		wg    sync.WaitGroup
	)
	for g := range togglers {
		target := roster[g%len(roster)]
		wg.Add(1)
		go func(p *player.Player) {
			defer wg.Done()
			<-start
			for range perGoTurns {
				// Error is expected sometimes (e.g. "need at least N players" when the
				// toggle briefly makes everyone ready). We only care that it never
				// races or deadlocks; correctness of the ready map is checked after.
				_ = l.ToggleReady(p, registry)
			}
		}(target)
	}

	runWithTimeout(t, 15*time.Second, func() {
		close(start)
		wg.Wait()
	})

	// The lobby never had enough players to start, so it must still be Waiting,
	// with an intact roster and every ready flag a well-defined bool.
	assert.True(t, l.IsWaiting(), "lobby must remain in Waiting; a start should never have succeeded")
	assert.Equal(t, members, l.CurrentPlayers(), "roster must be unchanged")
	for _, p := range roster {
		_ = l.IsReady(p) // must not race or panic
		assert.True(t, l.HasPlayer(p), "member %q vanished from roster", p.ID)
	}
}
