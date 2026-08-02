package lobby

import (
	"context"
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/player"
	"github.com/Pieczasz/terminal-card/internal/ratelimit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestManager_New(t *testing.T) {
	t.Parallel()
	m := NewManager(context.Background(), nil)
	leader := mockPlayer("p1", 1)

	l, err := m.New(leader, WithMaxPlayers(3), WithPrivate(false), WithCardGame(&db.Game{Name: "TestGame"}))
	require.NoError(t, err)
	assert.NotNil(t, l)

	assert.Equal(t, leader, l.Leader())
	assert.Equal(t, 3, l.MaxPlayers())
	assert.False(t, l.IsPrivate())

	assert.Len(t, l.Code(), 8)
	assert.Equal(t, l, m.FindLobbyByPlayer(leader))

	_, err = m.New(leader, WithCardGame(&db.Game{Name: "TestGame"}))
	require.ErrorContains(t, err, "already in a lobby")

	_, err = m.New(mockPlayer("p2", 2))
	require.ErrorContains(t, err, "card game is required")
}

func TestManager_JoinLobbyByCode(t *testing.T) {
	t.Parallel()
	m := NewManager(context.Background(), nil)
	leader := mockPlayer("p1", 1)
	guest1 := mockPlayer("g1", 2)
	guest2 := mockPlayer("g2", 3)

	l, err := m.New(leader, WithMaxPlayers(2), WithCardGame(&db.Game{Name: "TestGame"}))
	require.NoError(t, err)

	err = m.JoinLobbyByCode(l.Code(), guest1)
	require.NoError(t, err)
	assert.True(t, l.HasPlayer(guest1))
	assert.Equal(t, 2, l.CurrentPlayers())

	err = m.JoinLobbyByCode(l.Code(), guest2)
	require.ErrorContains(t, err, "this lobby is full")

	err = m.JoinLobbyByCode("FAKE12XX", guest2)
	require.ErrorContains(t, err, "lobby not found")

	err = m.JoinLobbyByCode(l.Code(), guest1)
	require.ErrorContains(t, err, "player is already in a lobby")
}

func TestManager_LeaveLobby(t *testing.T) {
	t.Parallel()
	m := NewManager(context.Background(), nil)
	leader := mockPlayer("p1", 1)
	guest1 := mockPlayer("g1", 2)
	guest2 := mockPlayer("g2", 3)

	l, _ := m.New(leader, WithMaxPlayers(3), WithCardGame(&db.Game{Name: "TestGame"}))
	require.NoError(t, m.JoinLobbyByCode(l.Code(), guest1))
	require.NoError(t, m.JoinLobbyByCode(l.Code(), guest2))

	m.LeaveLobby(guest1)
	assert.False(t, l.HasPlayer(guest1))
	assert.Equal(t, 2, l.CurrentPlayers())
	assert.Nil(t, m.FindLobbyByPlayer(guest1))

	m.LeaveLobby(leader)
	assert.False(t, l.HasPlayer(leader))
	assert.Equal(t, guest2, l.Leader())
	assert.Nil(t, m.FindLobbyByPlayer(leader))

	m.LeaveLobby(guest2)
	_, err := m.FindLobbyByCode(l.Code())
	require.ErrorContains(t, err, "lobby not found")
}

func TestManager_PublicLobbies(t *testing.T) {
	t.Parallel()
	m := NewManager(context.Background(), nil)

	p1 := mockPlayer("p1", 1)
	p2 := mockPlayer("p2", 2)
	p3 := mockPlayer("p3", 3)

	l1, _ := m.New(p1, WithPrivate(false), WithCardGame(&db.Game{Name: "TestGame"}))
	l2, _ := m.New(p2, WithPrivate(true), WithCardGame(&db.Game{Name: "TestGame"}))
	l3, _ := m.New(p3, WithPrivate(false), WithCardGame(&db.Game{Name: "TestGame"}))

	public := m.PublicLobbies(nil)
	assert.Len(t, public, 2)

	codes := []string{public[0].Code(), public[1].Code()}
	assert.Contains(t, codes, l1.Code())
	assert.Contains(t, codes, l3.Code())
	assert.NotContains(t, codes, l2.Code())
}

func TestManager_CodeCollisions(t *testing.T) {
	t.Parallel()
	m := NewManager(context.Background(), nil)

	// Exhausting a 36^8 code space to force a collision is not practical, so this only
	// pins the shape of what generateLobbyCode hands out.
	code, err := m.generateLobbyCode()
	require.NoError(t, err)
	assert.Len(t, code, 8)
}

func TestManager_PublicLobbiesCacheAndSorting(t *testing.T) {
	t.Parallel()
	m := NewManager(context.Background(), nil)

	p1 := mockPlayer("p1", 1)
	p2 := mockPlayer("p2", 2)

	_, _ = m.New(p1, WithPrivate(false), WithCardGame(&db.Game{Name: "CrazyEights"}))

	p2.DatabaseUser.Rankings = []db.Ranking{
		{Game: db.Game{Name: "CrazyEights"}, Elo: 3000},
	}

	public1 := m.PublicLobbies(p2)
	assert.Len(t, public1, 1)

	public2 := m.PublicLobbies(p2)
	assert.Equal(t, public1[0].Code(), public2[0].Code())
}

func TestManager_FindLobbyByPlayer_Cleanup(t *testing.T) {
	t.Parallel()
	m := NewManager(context.Background(), nil)
	leader := mockPlayer("p1", 1)

	l, _ := m.New(leader, WithCardGame(&db.Game{Name: "TestGame"}))

	// Manually mess up internal state to trigger the cleanup branch
	m.mu.Lock()
	l.mu.Lock()
	// Remove leader from lobby but keep in manager's map
	l.leader = mockPlayer("p2", 2)
	l.mu.Unlock()
	m.mu.Unlock()

	// This will notice player is not in lobby and delete from map
	found := m.FindLobbyByPlayer(leader)
	assert.Nil(t, found)
}

func TestManager_RejectMidGameJoin(t *testing.T) {
	t.Parallel()
	m := NewManager(context.Background(), nil)
	leader := mockPlayer("p1", 1)
	guest := mockPlayer("p2", 2)
	late := mockPlayer("p3", 3)

	cardGame := &db.Game{Name: "MockGame"}
	l, err := m.New(leader, WithMaxPlayers(4), WithCardGame(cardGame))
	require.NoError(t, err)
	require.NoError(t, m.JoinLobbyByCode(l.Code(), guest))

	registry := game.NewRegistry()
	mockRules := new(MockRules)
	mockRules.On("MinPlayers").Return(2)
	mockRules.On("MaxPlayers").Return(4)
	mockRules.On("InitialDeck").Return(deck.StandardDeck())
	mockRules.On("InitialDealCount").Return(5)
	mockRules.On("OnGameStart", mock.Anything).Return(nil)
	registerGame(registry, "MockGame", mockRules)

	require.NoError(t, l.ToggleReady(leader, registry))
	require.NoError(t, l.ToggleReady(guest, registry))
	assert.Equal(t, InGame, l.state)

	err = m.JoinLobbyByCode(l.Code(), late)
	require.ErrorContains(t, err, "not accepting players")
}

func TestManager_Kick(t *testing.T) {
	t.Parallel()
	m := NewManager(context.Background(), nil)
	leader := mockPlayer("p1", 1)
	guest := mockPlayer("p2", 2)
	guest2 := mockPlayer("p4", 4)
	intruder := mockPlayer("p3", 3)

	l, err := m.New(leader, WithMaxPlayers(4), WithCardGame(&db.Game{Name: "TestGame"}))
	require.NoError(t, err)
	require.NoError(t, m.JoinLobbyByCode(l.Code(), guest))
	require.NoError(t, m.JoinLobbyByCode(l.Code(), guest2))

	require.ErrorContains(t, m.Kick(guest, leader), "only the leader")
	require.ErrorContains(t, m.Kick(intruder, guest), "host is not in a lobby")
	require.ErrorContains(t, m.Kick(leader, leader), "cannot kick yourself")
	require.ErrorContains(t, m.Kick(nil, guest), "required")
	require.NoError(t, m.Kick(leader, guest))
	assert.False(t, l.HasPlayer(guest))
	assert.Nil(t, m.FindLobbyByPlayer(guest))
	assert.True(t, l.HasPlayer(guest2))
	require.ErrorContains(t, m.Kick(leader, guest), "player not in lobby")
}

func TestManager_JoinLobbyByCode_RateLimit(t *testing.T) {
	t.Parallel()
	m := NewManager(context.Background(), nil)
	m.joinLimiter = ratelimit.NewSlidingWindowLimiter(2, time.Minute)

	leader := mockPlayer("leader", 1)
	joiner := mockPlayer("joiner", 2)
	l, err := m.New(leader, WithMaxPlayers(4), WithCardGame(&db.Game{Name: "TestGame"}))
	require.NoError(t, err)

	require.NoError(t, m.JoinLobbyByCode(l.Code(), joiner))
	m.LeaveLobby(joiner)

	other := mockPlayer("other", 3)
	_, err = m.New(other, WithMaxPlayers(4), WithCardGame(&db.Game{Name: "TestGame2"}))
	require.NoError(t, err)

	// Second attempt still under limit.
	require.ErrorContains(t, m.JoinLobbyByCode("ZZZZZZZZ", joiner), "lobby not found")
	// Third attempt exceeds limit.
	err = m.JoinLobbyByCode("ZZZZZZZZ", joiner)
	require.ErrorContains(t, err, "too many join attempts")
}

func TestValidLobbyCode(t *testing.T) {
	t.Parallel()
	assert.True(t, ValidLobbyCode("ABCD1234"))
	assert.False(t, ValidLobbyCode("short"))
	assert.False(t, ValidLobbyCode("abcd1234"))
	assert.False(t, ValidLobbyCode("ABCD-234"))
}

func TestManager_PublicLobbies_Sorting(t *testing.T) {
	t.Parallel()
	m := NewManager(context.Background(), nil)

	p1 := mockPlayer("p1", 1) // average Elo = 1000
	p1.DatabaseUser.Rankings = []db.Ranking{{Game: db.Game{Name: "Game"}, Elo: 1000}}

	p2 := mockPlayer("p2", 2) // average Elo = 2000
	p2.DatabaseUser.Rankings = []db.Ranking{{Game: db.Game{Name: "Game"}, Elo: 2000}}

	p3 := mockPlayer("p3", 3)
	p3.DatabaseUser.Rankings = []db.Ranking{{Game: db.Game{Name: "Game"}, Elo: 3000}}

	l1, _ := m.New(p1, WithPrivate(false), WithCardGame(&db.Game{Name: "Game"}))
	l2, _ := m.New(p2, WithPrivate(false), WithCardGame(&db.Game{Name: "Game"}))

	// p3 has 3000, l2 has 2000 (diff 1000), l1 has 1000 (diff 2000)
	// So l2 should be first.
	public := m.PublicLobbies(p3)
	assert.Len(t, public, 2)
	assert.Equal(t, l2.Code(), public[0].Code())
	assert.Equal(t, l1.Code(), public[1].Code())
}

func TestManager_WaitForFinalizers(t *testing.T) {
	t.Parallel()
	m := NewManager(context.Background(), nil)

	assert.True(t, m.WaitForFinalizers(time.Second), "nothing in flight drains immediately")

	m = NewManager(context.Background(), nil)
	require.True(t, m.registerFinalizer())
	assert.False(t, m.WaitForFinalizers(50*time.Millisecond), "an in-flight write blocks the drain")

	m.finalizing.Done()
	assert.True(t, m.WaitForFinalizers(time.Second), "drains once the write completes")
}

func TestManager_WaitForFinalizers_StopsNewFinalizers(t *testing.T) {
	t.Parallel()
	m := NewManager(context.Background(), nil)
	require.True(t, m.registerFinalizer())

	drained := make(chan bool, 1)
	go func() {
		drained <- m.WaitForFinalizers(time.Second)
	}()

	assert.Eventually(t, func() bool {
		m.finalizerMu.Lock()
		defer m.finalizerMu.Unlock()
		return m.finalizersStopped
	}, time.Second, time.Millisecond)
	assert.False(t, m.registerFinalizer(), "a finalizer cannot register after shutdown starts")

	m.finalizing.Done()
	assert.True(t, <-drained, "shutdown waits for the registered finalizer")
}

// Shutdown may run before a manager was ever built.
func TestManager_WaitForFinalizers_NilReceiver(t *testing.T) {
	t.Parallel()
	var m *Manager
	assert.True(t, m.WaitForFinalizers(time.Second))
}

// FuzzJoinLobbyByCode covers a trust boundary: the code is typed by a remote SSH
// client, so arbitrary bytes reach the lookup. Nothing may panic, and no input may
// smuggle a player into a lobby they were not invited to.
func FuzzJoinLobbyByCode(f *testing.F) {
	f.Add("")
	f.Add("ABCD1234")
	f.Add("abcd1234")
	f.Add("../../etc/passwd")
	f.Add("' OR 1=1 --")
	f.Add("ABCD123\x00")

	f.Fuzz(func(t *testing.T, code string) {
		m := NewManager(context.Background(), nil)
		host := &player.Player{ID: "host", DatabaseUser: &db.User{Model: gorm.Model{ID: 1}}}
		l, err := m.New(host, WithCardGame(&db.Game{Name: "Poker"}), WithMaxPlayers(4))
		require.NoError(t, err)

		joiner := &player.Player{ID: "joiner", DatabaseUser: &db.User{Model: gorm.Model{ID: 2}}}
		err = m.JoinLobbyByCode(code, joiner)

		if code == l.Code() {
			require.NoError(t, err, "the real code must work")
			assert.True(t, l.HasPlayer(joiner))
			return
		}
		require.Error(t, err, "only the issued code may join")
		assert.False(t, l.HasPlayer(joiner), "a rejected code must not add the player")
		assert.Nil(t, m.FindLobbyByPlayer(joiner), "a rejected join leaves no membership")
	})
}
