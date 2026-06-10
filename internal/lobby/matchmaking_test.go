package lobby

import (
	"testing"

	"terminalcard/internal/db"
	"terminalcard/internal/deck"
	"terminalcard/internal/game"
	"terminalcard/internal/player"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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

	l, err := m.New(leader, WithMaxPlayers(3), WithPrivate(false))
	assert.NoError(t, err)
	assert.NotNil(t, l)

	assert.Equal(t, leader, l.Leader())
	assert.Equal(t, 3, l.MaxPlayers())
	assert.False(t, l.IsPrivate())

	assert.Len(t, l.Code(), 6)
	assert.Equal(t, l, m.FindLobbyByPlayer(leader))

	_, err = m.New(leader)
	assert.ErrorContains(t, err, "already in a lobby")
}

func TestManager_JoinLobbyByCode(t *testing.T) {
	t.Parallel()
	m := NewManager(nil)
	leader := mockPlayer("p1", 1)
	guest1 := mockPlayer("g1", 2)
	guest2 := mockPlayer("g2", 3)

	l, err := m.New(leader, WithMaxPlayers(2))
	assert.NoError(t, err)

	err = m.JoinLobbyByCode(l.Code(), guest1)
	assert.NoError(t, err)
	assert.True(t, l.HasPlayer(guest1))
	assert.Equal(t, 2, l.CurrentPlayers())

	err = m.JoinLobbyByCode(l.Code(), guest2)
	assert.ErrorContains(t, err, "this lobby is full")

	err = m.JoinLobbyByCode("FAKE12", guest2)
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

	l, _ := m.New(leader, WithMaxPlayers(3))
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

	l.SetPrivate(false)
	assert.False(t, l.IsPrivate())

	l.SetMaxPlayers(5)
	assert.Equal(t, 5, l.MaxPlayers())

	newGame := &db.Game{Name: "Poker"}
	l.SetCardGame(newGame)
	assert.Equal(t, newGame, l.options.cardGame)
}

func TestManager_PublicLobbies(t *testing.T) {
	t.Parallel()
	m := NewManager(nil)

	p1 := mockPlayer("p1", 1)
	p2 := mockPlayer("p2", 2)
	p3 := mockPlayer("p3", 3)

	l1, _ := m.New(p1, WithPrivate(false))
	l2, _ := m.New(p2, WithPrivate(true))
	l3, _ := m.New(p3, WithPrivate(false))

	public := m.PublicLobbies()
	assert.Len(t, public, 2)

	codes := []string{public[0].Code(), public[1].Code()}
	assert.Contains(t, codes, l1.Code())
	assert.Contains(t, codes, l3.Code())
	assert.NotContains(t, codes, l2.Code())
}
