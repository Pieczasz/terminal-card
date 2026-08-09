package hearts

import (
	"testing"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/hearts"
	"github.com/Pieczasz/terminal-card/internal/tui/router"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func testUser(id uint, name string) *db.User {
	return &db.User{Model: gorm.Model{ID: id}, Username: name}
}

func startedTable(t *testing.T) (*game.Engine, *Model) {
	t.Helper()
	players := []*game.Player{
		{ID: "1", UserID: 1, Name: "alice"},
		{ID: "2", UserID: 2, Name: "bob"},
		{ID: "3", UserID: 3, Name: "carol"},
		{ID: "4", UserID: 4, Name: "dave"},
	}
	engine := game.NewEngine(&logic.Rules{}, players, deck.StandardDeck())
	require.NoError(t, engine.Start())
	t.Cleanup(engine.Close)

	global := router.GlobalContext{User: testUser(1, "alice"), Width: 80, Height: 40}
	m, ok := New(global, engine).(*Model)
	require.True(t, ok)
	return engine, m
}

func TestSyncState_LoadsHeartsExtra(t *testing.T) {
	t.Parallel()
	_, m := startedTable(t)

	assert.Equal(t, 1, m.handNumber)
	assert.Contains(t, []logic.Stage{logic.StagePassing, logic.StageTrickPlay}, m.stage)
	assert.Len(t, m.seatOrder, 4)
	assert.Equal(t, "alice", m.seatNames["1"])
	assert.False(t, m.heartsBroken)
	assert.Equal(t, logic.DefaultTargetScore, 100)
}

func TestClose_ReleasesEngineSubscription(t *testing.T) {
	t.Parallel()
	engine, m := startedTable(t)
	require.Equal(t, 1, engine.Broadcaster().Len())
	m.Close()
	assert.Zero(t, engine.Broadcaster().Len())
}
