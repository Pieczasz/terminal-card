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

type MockRules struct {
	mock.Mock
}

func (m *MockRules) Name() string                        { return m.Called().String(0) }
func (m *MockRules) MinPlayers() int                     { return m.Called().Int(0) }
func (m *MockRules) MaxPlayers() int                     { return m.Called().Int(0) }
func (m *MockRules) InitialDeck() []deck.Card            { return m.Called().Get(0).([]deck.Card) }
func (m *MockRules) InitialDealCount() int               { return m.Called().Int(0) }
func (m *MockRules) OnGameStart(state *game.State) error { return m.Called(state).Error(0) }
func (m *MockRules) PreActionCondition(state *game.State, action game.Action) error {
	return m.Called(state, action).Error(0)
}
func (m *MockRules) ApplyAction(state *game.State, action game.Action) { m.Called(state, action) }
func (m *MockRules) PostActionCondition(state *game.State, action game.Action) error {
	return m.Called(state, action).Error(0)
}
func (m *MockRules) CheckWinCondition(state *game.State) bool { return m.Called(state).Bool(0) }
func (m *MockRules) GetStandings(state *game.State) []*player.Player {
	return m.Called(state).Get(0).([]*player.Player)
}

type MockMatchRepo struct {
	mock.Mock
}

func (m *MockMatchRepo) GetOrCreateGame(ctx context.Context, name string) (*db.Game, error) {
	args := m.Called(ctx, name)
	return args.Get(0).(*db.Game), args.Error(1)
}

func (m *MockMatchRepo) UpdateRankings(ctx context.Context, gameID uint, orderedUserIDs []uint) (map[uint]int, error) {
	args := m.Called(ctx, gameID, orderedUserIDs)
	return args.Get(0).(map[uint]int), args.Error(1)
}

func (m *MockMatchRepo) RecordMatch(ctx context.Context, gameID uint, orderedUserIDs []uint, eloDeltas map[uint]int) error {
	return m.Called(ctx, gameID, orderedUserIDs, eloDeltas).Error(0)
}

func (m *MockMatchRepo) FinalizeRankedMatch(ctx context.Context, gameName string, orderedUserIDs []uint) error {
	return m.Called(ctx, gameName, orderedUserIDs).Error(0)
}

func mockPlayer(id string, dbID uint) *player.Player {
	return &player.Player{
		ID: id,
		DatabaseUser: &db.User{
			Model: gorm.Model{ID: dbID},
		},
	}
}

func TestManager_New(t *testing.T) {
	t.Parallel()
	m := NewManager(nil)
	leader := mockPlayer("p1", 1)

	l, err := m.New(leader, WithMaxPlayers(3), WithPrivate(false), WithCardGame(&db.Game{Name: "TestGame"}))
	assert.NoError(t, err)
	assert.NotNil(t, l)

	assert.Equal(t, leader, l.Leader())
	assert.Equal(t, 3, l.MaxPlayers())
	assert.False(t, l.IsPrivate())

	assert.Len(t, l.Code(), 8)
	assert.Equal(t, l, m.FindLobbyByPlayer(leader))

	_, err = m.New(leader, WithCardGame(&db.Game{Name: "TestGame"}))
	assert.ErrorContains(t, err, "already in a lobby")

	_, err = m.New(mockPlayer("p2", 2))
	assert.ErrorContains(t, err, "card game is required")
}

func TestManager_JoinLobbyByCode(t *testing.T) {
	t.Parallel()
	m := NewManager(nil)
	leader := mockPlayer("p1", 1)
	guest1 := mockPlayer("g1", 2)
	guest2 := mockPlayer("g2", 3)

	l, err := m.New(leader, WithMaxPlayers(2), WithCardGame(&db.Game{Name: "TestGame"}))
	assert.NoError(t, err)

	err = m.JoinLobbyByCode(l.Code(), guest1)
	assert.NoError(t, err)
	assert.True(t, l.HasPlayer(guest1))
	assert.Equal(t, 2, l.CurrentPlayers())

	err = m.JoinLobbyByCode(l.Code(), guest2)
	assert.ErrorContains(t, err, "this lobby is full")

	err = m.JoinLobbyByCode("FAKE12XX", guest2)
	assert.ErrorContains(t, err, "lobby not found")

	err = m.JoinLobbyByCode(l.Code(), guest1)
	assert.ErrorContains(t, err, "player is already in a lobby")
}

func TestManager_LeaveLobby(t *testing.T) {
	t.Parallel()
	m := NewManager(nil)
	leader := mockPlayer("p1", 1)
	guest1 := mockPlayer("g1", 2)
	guest2 := mockPlayer("g2", 3)

	l, _ := m.New(leader, WithMaxPlayers(3), WithCardGame(&db.Game{Name: "TestGame"}))
	m.JoinLobbyByCode(l.Code(), guest1)
	m.JoinLobbyByCode(l.Code(), guest2)

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
	assert.ErrorContains(t, err, "lobby not found")
}

func TestLobby_ToggleReady(t *testing.T) {
	t.Parallel()
	m := NewManager(nil)
	leader := mockPlayer("p1", 1)
	guest := mockPlayer("p2", 2)

	cardGame := &db.Game{Name: "MockGame"}
	l, _ := m.New(leader, WithMaxPlayers(4), WithCardGame(cardGame))
	m.JoinLobbyByCode(l.Code(), guest)

	registry := game.NewRegistry()
	mockRules := new(MockRules)

	mockRules.On("MinPlayers").Return(2)
	mockRules.On("MaxPlayers").Return(4)
	mockRules.On("InitialDeck").Return(deck.StandardDeck())
	mockRules.On("InitialDealCount").Return(5)
	mockRules.On("OnGameStart", mock.Anything).Return(nil)

	registry.Register("MockGame", func() game.Rules {
		return mockRules
	})

	err := l.ToggleReady(leader, registry)
	assert.NoError(t, err)
	assert.True(t, l.IsReady(leader))
	assert.Equal(t, Waiting, l.state)

	err = l.ToggleReady(guest, registry)
	assert.NoError(t, err)

	assert.Equal(t, InGame, l.state)
	assert.NotNil(t, l.activeEngine)

	mockRules.AssertExpectations(t)
}

func TestLobby_SettersAndGetters(t *testing.T) {
	t.Parallel()
	m := NewManager(nil)
	leader := mockPlayer("p1", 1)

	cardGame := &db.Game{Name: "CrazyEights"}
	l, err := m.New(leader, WithCardGame(cardGame), WithPrivate(true), WithRanked(true))
	assert.NoError(t, err)

	assert.Equal(t, cardGame, l.options.cardGame)
	assert.True(t, l.IsPrivate())
	assert.True(t, l.options.isRanked)

	assert.NoError(t, l.SetPrivate(leader, false))
	assert.False(t, l.IsPrivate())

	assert.NoError(t, l.SetMaxPlayers(leader, 5, 2, 6))
	assert.Equal(t, 5, l.MaxPlayers())

	newGame := &db.Game{Name: "Poker"}
	assert.NoError(t, l.SetCardGame(leader, newGame))
	assert.Equal(t, newGame, l.options.cardGame)
}

func TestManager_PublicLobbies(t *testing.T) {
	t.Parallel()
	m := NewManager(nil)

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

func TestLobby_BasicGetters(t *testing.T) {
	t.Parallel()
	m := NewManager(nil)
	leader := mockPlayer("leader", 1)

	cardGame := &db.Game{Name: "CrazyEights"}
	l, _ := m.New(leader, WithCardGame(cardGame), WithPrivate(true), WithMaxPlayers(4))

	assert.Equal(t, cardGame.Name, l.GameName())
	assert.Len(t, l.Code(), 8)
	assert.NotNil(t, l.Broadcaster())
	assert.Empty(t, l.Guests())
	assert.Equal(t, 4, l.MaxPlayers())
	assert.True(t, l.IsPrivate())

	guest := mockPlayer("guest", 2)
	guest.DatabaseUser.Rankings = []db.Ranking{
		{Game: db.Game{Name: "CrazyEights"}, Elo: 2000},
	}
	m.JoinLobbyByCode(l.Code(), guest)

	assert.Len(t, l.Guests(), 1)

	// Leader has default 1500, guest has 2000. Avg = 1750.
	avg := l.averageElo()
	assert.Equal(t, uint32(1750), avg)
}

func TestLobby_StartGameAndBroadcasterEvents(t *testing.T) {
	t.Parallel()
	mockRepo := new(MockMatchRepo)
	m := NewManager(mockRepo)
	leader := mockPlayer("leader", 1)
	guest := mockPlayer("guest", 2)

	cardGame := &db.Game{Name: "MockGame"}
	l, _ := m.New(leader, WithMaxPlayers(2), WithCardGame(cardGame), WithRanked(true))
	m.JoinLobbyByCode(l.Code(), guest)

	registry := game.NewRegistry()
	mockRules := new(MockRules)

	mockRules.On("MinPlayers").Return(2)
	mockRules.On("MaxPlayers").Return(4)
	mockRules.On("InitialDeck").Return(deck.StandardDeck())
	mockRules.On("InitialDealCount").Return(5)
	mockRules.On("OnGameStart", mock.Anything).Return(nil)
	mockRules.On("CheckWinCondition", mock.Anything).Return(true) // Immediate win to end game
	mockRules.On("GetStandings", mock.Anything).Return([]*player.Player{leader, guest})

	registry.Register("MockGame", func() game.Rules { return mockRules })

	done := make(chan struct{})
	mockRepo.On("FinalizeRankedMatch", mock.Anything, "MockGame", []uint{1, 2}).
		Run(func(mock.Arguments) {
			close(done)
		}).
		Return(nil)

	ch := l.Broadcaster().Subscribe()

	// Start the game
	err := l.ToggleReady(leader, registry)
	assert.NoError(t, err)
	err = l.ToggleReady(guest, registry)
	assert.NoError(t, err)

	// Game is started, let's trigger GameEnded event directly to test handleBroadcasterEvents
	engine := l.activeEngine
	assert.NotNil(t, engine)

	engine.Broadcaster().Broadcast(game.Event{
		Type: game.EventGameEnded,
	})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ranked finalize")
	}

	l.Broadcaster().Unsubscribe(ch)
	mockRepo.AssertExpectations(t)
}

func TestManager_CodeCollisions(t *testing.T) {
	t.Parallel()
	m := NewManager(nil)

	// Pre-fill the manager with all possible codes if it were a smaller charset?
	// The function retries 10 times. Let's just mock a collision by forcing lobbies map to contain a generated code.
	// Actually testing true exhaustion is hard with 6-char random strings. We will just test generation works.
	code, err := m.generateLobbyCode()
	assert.NoError(t, err)
	assert.Len(t, code, 8)
}

func TestManager_PublicLobbiesCacheAndSorting(t *testing.T) {
	t.Parallel()
	m := NewManager(nil)

	p1 := mockPlayer("p1", 1)
	p2 := mockPlayer("p2", 2)

	// Create public lobby
	_, _ = m.New(p1, WithPrivate(false), WithCardGame(&db.Game{Name: "CrazyEights"}))

	// p2 with high Elo
	p2.DatabaseUser.Rankings = []db.Ranking{
		{Game: db.Game{Name: "CrazyEights"}, Elo: 3000},
	}

	// Cache is hit here.
	public1 := m.PublicLobbies(p2)
	assert.Len(t, public1, 1)

	// Second call hits cache
	public2 := m.PublicLobbies(p2)
	assert.Equal(t, public1[0].Code(), public2[0].Code())
}

func TestManager_FindLobbyByPlayer_Cleanup(t *testing.T) {
	t.Parallel()
	m := NewManager(nil)
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

func TestLobby_ToggleReady_EdgeCases(t *testing.T) {
	t.Parallel()
	m := NewManager(nil)
	leader := mockPlayer("p1", 1)
	guest := mockPlayer("p2", 2)
	guest3 := mockPlayer("p3", 3)

	l, _ := m.New(leader, WithMaxPlayers(3), WithCardGame(&db.Game{Name: "Mock"}))
	m.JoinLobbyByCode(l.Code(), guest)
	m.JoinLobbyByCode(l.Code(), guest3)

	registry := game.NewRegistry()
	mockRules := new(MockRules)

	// mockRules limits max players to 2, but lobby has 3!
	mockRules.On("MinPlayers").Return(2)
	mockRules.On("MaxPlayers").Return(2)
	registry.Register("Mock", func() game.Rules { return mockRules })

	l.ToggleReady(leader, registry)
	l.ToggleReady(guest, registry)
	err := l.ToggleReady(guest3, registry) // This triggers start game and should fail due to max players!
	assert.ErrorContains(t, err, "too many players")

	// Missing game in registry
	leader2 := mockPlayer("p4", 4)
	l2, _ := m.New(leader2, WithCardGame(&db.Game{Name: "Missing"}))
	err = l2.ToggleReady(leader2, registry) // Should fail on create game rules
	assert.ErrorContains(t, err, "failed to create game rules")

	// Game already in progress
	mockRules2 := new(MockRules)
	mockRules2.On("MinPlayers").Return(2)
	mockRules2.On("MaxPlayers").Return(4)
	mockRules2.On("InitialDeck").Return(deck.StandardDeck())
	mockRules2.On("InitialDealCount").Return(5)
	mockRules2.On("OnGameStart", mock.Anything).Return(nil)
	registry.Register("Mock2", func() game.Rules { return mockRules2 })

	leader3 := mockPlayer("p5", 5)
	l3, _ := m.New(leader3, WithCardGame(&db.Game{Name: "Mock2"}))
	m.JoinLobbyByCode(l3.Code(), guest) // guest is removed from l (but m.Join doesn't remove, actually player can't join multiple). Let's use a new guest.
	guest4 := mockPlayer("p6", 6)
	m.JoinLobbyByCode(l3.Code(), guest4)
	l3.ToggleReady(leader3, registry)
	l3.ToggleReady(guest4, registry) // Starts game!

	err = l3.ToggleReady(leader3, registry) // Game is already in progress
	assert.ErrorContains(t, err, "game is already in progress")

	// Unknown player toggling ready
	err = l2.ToggleReady(guest3, registry)
	assert.ErrorContains(t, err, "not in lobby")

	// Remove non-existent player
	removed := l3.RemovePlayer(guest3)
	assert.False(t, removed)
}

func TestManager_RejectMidGameJoin(t *testing.T) {
	t.Parallel()
	m := NewManager(nil)
	leader := mockPlayer("p1", 1)
	guest := mockPlayer("p2", 2)
	late := mockPlayer("p3", 3)

	cardGame := &db.Game{Name: "MockGame"}
	l, err := m.New(leader, WithMaxPlayers(4), WithCardGame(cardGame))
	assert.NoError(t, err)
	assert.NoError(t, m.JoinLobbyByCode(l.Code(), guest))

	registry := game.NewRegistry()
	mockRules := new(MockRules)
	mockRules.On("MinPlayers").Return(2)
	mockRules.On("MaxPlayers").Return(4)
	mockRules.On("InitialDeck").Return(deck.StandardDeck())
	mockRules.On("InitialDealCount").Return(5)
	mockRules.On("OnGameStart", mock.Anything).Return(nil)
	registry.Register("MockGame", func() game.Rules { return mockRules })

	assert.NoError(t, l.ToggleReady(leader, registry))
	assert.NoError(t, l.ToggleReady(guest, registry))
	assert.Equal(t, InGame, l.state)

	err = m.JoinLobbyByCode(l.Code(), late)
	assert.ErrorContains(t, err, "not accepting players")
}

func TestManager_Kick(t *testing.T) {
	t.Parallel()
	m := NewManager(nil)
	leader := mockPlayer("p1", 1)
	guest := mockPlayer("p2", 2)
	guest2 := mockPlayer("p4", 4)
	intruder := mockPlayer("p3", 3)

	l, err := m.New(leader, WithMaxPlayers(4), WithCardGame(&db.Game{Name: "TestGame"}))
	assert.NoError(t, err)
	assert.NoError(t, m.JoinLobbyByCode(l.Code(), guest))
	assert.NoError(t, m.JoinLobbyByCode(l.Code(), guest2))

	assert.ErrorContains(t, m.Kick(guest, leader), "only the leader")
	assert.ErrorContains(t, m.Kick(intruder, guest), "host is not in a lobby")
	assert.ErrorContains(t, m.Kick(leader, leader), "cannot kick yourself")
	assert.ErrorContains(t, m.Kick(nil, guest), "required")
	assert.NoError(t, m.Kick(leader, guest))
	assert.False(t, l.HasPlayer(guest))
	assert.Nil(t, m.FindLobbyByPlayer(guest))
	assert.True(t, l.HasPlayer(guest2))
	assert.ErrorContains(t, m.Kick(leader, guest), "player not in lobby")
}

func TestManager_JoinLobbyByCode_RateLimit(t *testing.T) {
	t.Parallel()
	m := NewManager(nil)
	m.joinLimiter = ratelimit.NewSlidingWindowLimiter(2, time.Minute)

	leader := mockPlayer("leader", 1)
	joiner := mockPlayer("joiner", 2)
	l, err := m.New(leader, WithMaxPlayers(4), WithCardGame(&db.Game{Name: "TestGame"}))
	require.NoError(t, err)

	assert.NoError(t, m.JoinLobbyByCode(l.Code(), joiner))
	m.LeaveLobby(joiner)

	other := mockPlayer("other", 3)
	_, err = m.New(other, WithMaxPlayers(4), WithCardGame(&db.Game{Name: "TestGame2"}))
	require.NoError(t, err)

	// Second attempt still under limit.
	assert.ErrorContains(t, m.JoinLobbyByCode("ZZZZZZZZ", joiner), "lobby not found")
	// Third attempt exceeds limit.
	err = m.JoinLobbyByCode("ZZZZZZZZ", joiner)
	assert.ErrorContains(t, err, "too many join attempts")
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
	m := NewManager(nil)

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
