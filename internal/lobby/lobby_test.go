package lobby

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/player"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// registerGame registers rules under a display name. These tests only ever look
// games up by name, so the slug just has to be present and distinct.
func registerGame(r *game.Registry, name string, rules game.Rules) {
	r.RegisterModule(game.Module{
		Name:    name,
		Slug:    strings.ToLower(name),
		Factory: func() game.Rules { return rules },
	})
}

type MockRules struct {
	mock.Mock
}

func (m *MockRules) MinPlayers() int                     { return m.Called().Int(0) }
func (m *MockRules) MaxPlayers() int                     { return m.Called().Int(0) }
func (m *MockRules) InitialDeck() []deck.Card            { return m.Called().Get(0).([]deck.Card) }
func (m *MockRules) InitialDealCount() int               { return m.Called().Int(0) }
func (m *MockRules) OnGameStart(state *game.State) error { return m.Called(state).Error(0) }
func (m *MockRules) ValidateAction(state *game.State, action game.Action) error {
	return m.Called(state, action).Error(0)
}
func (m *MockRules) ApplyAction(state *game.State, action game.Action) { m.Called(state, action) }
func (m *MockRules) AfterAction(state *game.State, action game.Action) error {
	return m.Called(state, action).Error(0)
}
func (m *MockRules) CheckWinCondition(state *game.State) bool { return m.Called(state).Bool(0) }
func (m *MockRules) Standings(state *game.State) []*player.Player {
	return m.Called(state).Get(0).([]*player.Player)
}

type MockMatchRepo struct {
	mock.Mock
}

func (m *MockMatchRepo) GetOrCreateGame(ctx context.Context, name string) (*db.Game, error) {
	args := m.Called(ctx, name)
	return args.Get(0).(*db.Game), args.Error(1)
}

func (m *MockMatchRepo) RecordMatch(
	ctx context.Context, gameID uint, orderedUserIDs []uint, eloDeltas map[uint]int, ranked bool,
) error {
	return m.Called(ctx, gameID, orderedUserIDs, eloDeltas, ranked).Error(0)
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
func TestLobby_ToggleReady(t *testing.T) {
	t.Parallel()
	m := NewManager(context.Background(), nil)
	leader := mockPlayer("p1", 1)
	guest := mockPlayer("p2", 2)

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

	err = l.ToggleReady(leader, registry)
	require.NoError(t, err)
	assert.True(t, l.IsReady(leader))
	assert.Equal(t, Waiting, l.state)

	err = l.ToggleReady(guest, registry)
	require.NoError(t, err)

	assert.Equal(t, InGame, l.state)
	assert.NotNil(t, l.activeEngine)

	mockRules.AssertExpectations(t)
}

func TestLobby_SettersAndGetters(t *testing.T) {
	t.Parallel()
	m := NewManager(context.Background(), nil)
	leader := mockPlayer("p1", 1)

	cardGame := &db.Game{Name: "CrazyEights"}
	l, err := m.New(leader, WithCardGame(cardGame), WithPrivate(true), WithRanked(true))
	require.NoError(t, err)

	assert.Equal(t, cardGame, l.options.cardGame)
	assert.True(t, l.IsPrivate())
	assert.True(t, l.IsRanked())

	require.NoError(t, l.SetPrivate(leader, false))
	assert.False(t, l.IsPrivate())

	require.NoError(t, l.SetRanked(leader, false))
	assert.False(t, l.IsRanked())

	require.NoError(t, l.SetMaxPlayers(leader, 5, 2, 6))
	assert.Equal(t, 5, l.MaxPlayers())

	newGame := &db.Game{Name: "Poker"}
	require.NoError(t, l.SetCardGame(leader, newGame))
	assert.Equal(t, newGame, l.options.cardGame)
}

func TestLobby_DefaultCasual(t *testing.T) {
	t.Parallel()
	m := NewManager(context.Background(), nil)
	leader := mockPlayer("leader", 1)
	l, err := m.New(leader, WithCardGame(&db.Game{Name: "TestGame"}))
	require.NoError(t, err)
	assert.False(t, l.IsRanked(), "new lobbies default to casual to limit Elo farming under open registration")
}

func TestLobby_BasicGetters(t *testing.T) {
	t.Parallel()
	m := NewManager(context.Background(), nil)
	leader := mockPlayer("leader", 1)

	cardGame := &db.Game{Name: "CrazyEights"}
	l, err := m.New(leader, WithCardGame(cardGame), WithPrivate(true), WithMaxPlayers(4))
	require.NoError(t, err)

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
	require.NoError(t, m.JoinLobbyByCode(l.Code(), guest))

	assert.Len(t, l.Guests(), 1)

	// Leader has default 1500, guest has 2000. Avg = 1750.
	avg := l.averageElo()
	assert.Equal(t, uint32(1750), avg)
}

func TestLobby_StartGameAndBroadcasterEvents(t *testing.T) {
	t.Parallel()
	mockRepo := new(MockMatchRepo)
	m := NewManager(context.Background(), mockRepo)
	leader := mockPlayer("leader", 1)
	guest := mockPlayer("guest", 2)

	cardGame := &db.Game{Name: "MockGame"}
	l, err := m.New(leader, WithMaxPlayers(2), WithCardGame(cardGame), WithRanked(true))
	require.NoError(t, err)
	require.NoError(t, m.JoinLobbyByCode(l.Code(), guest))

	registry := game.NewRegistry()
	mockRules := new(MockRules)

	mockRules.On("MinPlayers").Return(2)
	mockRules.On("MaxPlayers").Return(4)
	mockRules.On("InitialDeck").Return(deck.StandardDeck())
	mockRules.On("InitialDealCount").Return(5)
	mockRules.On("OnGameStart", mock.Anything).Return(nil)
	mockRules.On("CheckWinCondition", mock.Anything).Return(true) // Immediate win to end game
	mockRules.On("Standings", mock.Anything).Return([]*player.Player{leader, guest})

	registerGame(registry, "MockGame", mockRules)

	done := make(chan struct{})
	mockRepo.On("FinalizeRankedMatch", mock.Anything, "MockGame", []uint{1, 2}).
		Run(func(mock.Arguments) {
			close(done)
		}).
		Return(nil)

	ch := l.Broadcaster().Subscribe()

	err = l.ToggleReady(leader, registry)
	require.NoError(t, err)
	err = l.ToggleReady(guest, registry)
	require.NoError(t, err)

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

// A casual game is still a game the players want to find in their history, so it
// is recorded; only the Elo write is reserved for ranked lobbies.
func TestLobby_CasualGameIsRecordedWithoutElo(t *testing.T) {
	t.Parallel()
	mockRepo := new(MockMatchRepo)
	m := NewManager(context.Background(), mockRepo)
	leader := mockPlayer("leader", 1)
	guest := mockPlayer("guest", 2)

	cardGame := &db.Game{Name: "MockGame"}
	l, err := m.New(leader, WithMaxPlayers(2), WithCardGame(cardGame), WithRanked(false))
	require.NoError(t, err)
	require.NoError(t, m.JoinLobbyByCode(l.Code(), guest))

	registry := game.NewRegistry()
	mockRules := new(MockRules)
	mockRules.On("MinPlayers").Return(2)
	mockRules.On("MaxPlayers").Return(4)
	mockRules.On("InitialDeck").Return(deck.StandardDeck())
	mockRules.On("InitialDealCount").Return(5)
	mockRules.On("OnGameStart", mock.Anything).Return(nil)
	mockRules.On("CheckWinCondition", mock.Anything).Return(true)
	mockRules.On("Standings", mock.Anything).Return([]*player.Player{leader, guest})
	registerGame(registry, "MockGame", mockRules)

	done := make(chan struct{})
	mockRepo.On("GetOrCreateGame", mock.Anything, "MockGame").Return(&db.Game{Model: gorm.Model{ID: 7}}, nil)
	mockRepo.On("RecordMatch", mock.Anything, uint(7), []uint{1, 2}, map[uint]int(nil), false).
		Run(func(mock.Arguments) { close(done) }).
		Return(nil)

	require.NoError(t, l.ToggleReady(leader, registry))
	require.NoError(t, l.ToggleReady(guest, registry))

	engine := l.activeEngine
	require.NotNil(t, engine)
	engine.Broadcaster().Broadcast(game.Event{Type: game.EventGameEnded})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the casual match to be recorded")
	}

	mockRepo.AssertExpectations(t)
	mockRepo.AssertNotCalled(t, "FinalizeRankedMatch", mock.Anything, mock.Anything, mock.Anything)
}

func TestLobby_ToggleReady_EdgeCases(t *testing.T) {
	t.Parallel()
	m := NewManager(context.Background(), nil)
	leader := mockPlayer("p1", 1)
	guest := mockPlayer("p2", 2)
	guest3 := mockPlayer("p3", 3)

	l, err := m.New(leader, WithMaxPlayers(3), WithCardGame(&db.Game{Name: "Mock"}))
	require.NoError(t, err)
	require.NoError(t, m.JoinLobbyByCode(l.Code(), guest))
	require.NoError(t, m.JoinLobbyByCode(l.Code(), guest3))

	registry := game.NewRegistry()
	mockRules := new(MockRules)

	// mockRules limits max players to 2, but lobby has 3!
	mockRules.On("MinPlayers").Return(2)
	mockRules.On("MaxPlayers").Return(2)
	registerGame(registry, "Mock", mockRules)

	require.NoError(t, l.ToggleReady(leader, registry))
	require.NoError(t, l.ToggleReady(guest, registry))
	err = l.ToggleReady(guest3, registry) // This triggers start game and should fail due to max players!
	require.ErrorContains(t, err, "too many players")

	// Missing game in registry
	leader2 := mockPlayer("p4", 4)
	l2, err := m.New(leader2, WithCardGame(&db.Game{Name: "Missing"}))
	require.NoError(t, err)
	err = l2.ToggleReady(leader2, registry) // Should fail on create game rules
	require.ErrorContains(t, err, "failed to create game rules")

	// Game already in progress
	mockRules2 := new(MockRules)
	mockRules2.On("MinPlayers").Return(2)
	mockRules2.On("MaxPlayers").Return(4)
	mockRules2.On("InitialDeck").Return(deck.StandardDeck())
	mockRules2.On("InitialDealCount").Return(5)
	mockRules2.On("OnGameStart", mock.Anything).Return(nil)
	registerGame(registry, "Mock2", mockRules2)

	leader3 := mockPlayer("p5", 5)
	l3, err := m.New(leader3, WithCardGame(&db.Game{Name: "Mock2"}))
	require.NoError(t, err)
	_ = m.JoinLobbyByCode(l3.Code(), guest) // guest is already in lobby l, so this join is expected to fail.
	guest4 := mockPlayer("p6", 6)
	require.NoError(t, m.JoinLobbyByCode(l3.Code(), guest4))
	require.NoError(t, l3.ToggleReady(leader3, registry))
	require.NoError(t, l3.ToggleReady(guest4, registry)) // Starts game!

	err = l3.ToggleReady(leader3, registry) // Game is already in progress
	require.ErrorContains(t, err, "game is already in progress")

	// Unknown player toggling ready
	err = l2.ToggleReady(guest3, registry)
	require.ErrorContains(t, err, "not in lobby")

	// Remove non-existent player
	removed := l3.RemovePlayer(guest3)
	assert.False(t, removed)
}
