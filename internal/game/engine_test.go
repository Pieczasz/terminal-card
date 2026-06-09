package game

import (
	"testing"

	"terminalcard/internal/deck"
	"terminalcard/internal/player"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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

func setupMockRules() *MockRules {
	m := new(MockRules)
	m.On("InitialDeck").Return(deck.StandardDeck())
	m.On("InitialDealCount").Return(5)
	m.On("OnGameStart", mock.Anything).Return(nil)
	return m
}

func TestEngine_Start(t *testing.T) {
	t.Parallel()
	players := []*player.Player{{ID: "p1"}, {ID: "p2"}}
	m := setupMockRules()
	engine := NewGameEngine(m, players, deck.StandardDeck())

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
	engine := NewGameEngine(m, players, deck.StandardDeck())
	engine.Start()

	currentPlayerID := engine.CurrentPlayer().ID
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

	assert.Equal(t, otherPlayerID, engine.CurrentPlayer().ID)

	m.AssertExpectations(t)
}

func TestEngine_RemovePlayer(t *testing.T) {
	t.Parallel()
	players := []*player.Player{{ID: "p1"}, {ID: "p2"}, {ID: "p3"}}
	m := setupMockRules()
	engine := NewGameEngine(m, players, deck.StandardDeck())
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
