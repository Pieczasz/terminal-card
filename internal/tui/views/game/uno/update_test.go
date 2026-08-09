package uno

import (
	"strconv"
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/uno"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	gameview "github.com/Pieczasz/terminal-card/internal/tui/views/game"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUpdate_Navigation(t *testing.T) {
	t.Parallel()
	m := Model{Session: gameview.Session{
		Base: gameview.BaseState{
			Hand: []deck.Card{
				{Rank: deck.Two, Suit: logic.ColorRed},
				{Rank: deck.Three, Suit: logic.ColorBlue},
				{Rank: deck.Four, Suit: logic.ColorGreen},
			},
		},
	}}

	msg := tea.KeyPressMsg{Code: rune("l"[0]), Text: "l"}
	newM, _ := m.Update(msg)
	assert.Equal(t, 1, newM.(*Model).Selected)

	msg = tea.KeyPressMsg{Code: rune("h"[0]), Text: "h"}
	newM, _ = newM.Update(msg)
	assert.Equal(t, 0, newM.(*Model).Selected)
}

func TestUpdate_ColorPicking(t *testing.T) {
	t.Parallel()
	m := Model{pickingColor: true, colorCursor: 0}

	msg := tea.KeyPressMsg{Code: rune("l"[0]), Text: "l"}
	newM, _ := m.Update(msg)
	assert.Equal(t, 1, newM.(*Model).colorCursor)

	msg = tea.KeyPressMsg{Code: rune("j"[0]), Text: "j"}
	newM, _ = newM.Update(msg)
	assert.Equal(t, 3, newM.(*Model).colorCursor)
}

// tableOnTurn seats the view as whichever player the engine put on turn, so the
// test does not depend on where the deal landed.
func tableOnTurn(t *testing.T) (*game.Engine, *Model) {
	t.Helper()
	players := []*game.Player{
		{ID: "1", UserID: 1, Name: "alice"},
		{ID: "2", UserID: 2, Name: "bob"},
		{ID: "3", UserID: 3, Name: "carol"},
	}
	engine := game.NewEngine(&logic.Rules{}, players, logic.InitialDeck())
	require.NoError(t, engine.Start())
	t.Cleanup(engine.Close)

	id, err := strconv.ParseUint(engine.CurrentPlayerID(), 10, 64)
	require.NoError(t, err)

	global := router.GlobalContext{User: &db.User{Model: gorm.Model{ID: uint(id)}, Username: "hero"}}
	m, ok := New(global, engine).(*Model)
	require.True(t, ok)
	require.True(t, m.Base.MyTurn, "the view has to be bound to the seat on turn")
	return engine, m
}

// The colour picker is a modal over the hero's own turn. It used to survive the
// turn being taken by the clock, leaving a picker on screen with nothing left to
// confirm and the table hidden behind it.
func TestSyncState_ClosesTheColourPickerWhenTheTurnIsLost(t *testing.T) {
	t.Parallel()
	engine, m := tableOnTurn(t)

	m.pickingColor = true
	m.colorCursor = 2
	require.NoError(t, engine.SubmitAction(m.Bound.PlayerID(), logic.ActionDrawCard{}))
	m.syncState()

	require.False(t, m.Base.MyTurn, "drawing passes the turn on")
	assert.False(t, m.pickingColor, "the picker cannot outlive the turn it belongs to")
}

func TestSyncState_KeepsTheColourPickerWhileTheTurnIsStillYours(t *testing.T) {
	t.Parallel()
	_, m := tableOnTurn(t)

	m.pickingColor = true
	m.syncState()

	assert.True(t, m.pickingColor, "a refresh mid-turn must not close the picker")
}

func TestClose_ReleasesEngineSubscription(t *testing.T) {
	t.Parallel()

	players := []*game.Player{
		{ID: "1", UserID: 1, Name: "alice"},
		{ID: "2", UserID: 2, Name: "bob"},
	}
	engine := game.NewEngine(&logic.Rules{}, players, logic.InitialDeck())
	require.NoError(t, engine.Start())
	t.Cleanup(engine.Close)

	global := router.GlobalContext{User: &db.User{Model: gorm.Model{ID: 1}, Username: "alice"}}
	m, ok := New(global, engine).(*Model)
	require.True(t, ok)
	require.Equal(t, 1, engine.Broadcaster().Len())

	listen := m.Listen()
	done := make(chan tea.Msg, 1)
	go func() { done <- listen() }()

	m.Close()
	assert.Zero(t, engine.Broadcaster().Len())

	select {
	case msg := <-done:
		assert.Nil(t, msg)
	case <-time.After(2 * time.Second):
		t.Fatal("listener goroutine did not return after Close")
	}

	m.Close()
}
