package crazyeight

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/crazyeight"
	"github.com/Pieczasz/terminal-card/internal/tui/router"

	"github.com/Pieczasz/terminal-card/internal/deck"
	gameview "github.com/Pieczasz/terminal-card/internal/tui/views/game"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
)

func TestUpdate_Navigation(t *testing.T) {
	t.Parallel()
	m := Model{
		Base: gameview.BaseState{
			Hand: []deck.Card{
				{Rank: deck.Two, Suit: deck.Spades},
				{Rank: deck.Three, Suit: deck.Hearts},
				{Rank: deck.Four, Suit: deck.Clubs},
			},
		}}

	// Right
	msg := tea.KeyPressMsg{Code: rune("l"[0]), Text: "l"}
	newM, _ := m.Update(msg)
	assert.Equal(t, 1, newM.(*Model).Selected)

	// Left
	msg = tea.KeyPressMsg{Code: rune("h"[0]), Text: "h"}
	newM, _ = newM.Update(msg)
	assert.Equal(t, 0, newM.(*Model).Selected)
}

func TestUpdate_SuitPicking(t *testing.T) {
	t.Parallel()
	m := Model{
		pickingSuit: true,
		suitCursor:  0,
	}

	// Right (adds 1 to cursor)
	msg := tea.KeyPressMsg{Code: rune("l"[0]), Text: "l"}
	newM, _ := m.Update(msg)
	assert.Equal(t, 1, newM.(*Model).suitCursor)

	// Down (adds 2 to cursor)
	msg = tea.KeyPressMsg{Code: rune("j"[0]), Text: "j"}
	newM, _ = newM.Update(msg)
	assert.Equal(t, 3, newM.(*Model).suitCursor)
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
	engine := game.NewEngine(&logic.Rules{}, players, deck.StandardDeck())
	require.NoError(t, engine.Start())
	t.Cleanup(engine.Close)

	id, err := strconv.ParseUint(engine.CurrentPlayerID(), 10, 64)
	require.NoError(t, err)

	global := router.GlobalContext{User: &db.User{ID: uint(id), Username: "hero"}}
	m, ok := New(global, engine).(*Model)
	require.True(t, ok)
	require.True(t, m.Base.MyTurn, "the view has to be bound to the seat on turn")
	return engine, m
}

// The suit picker is a modal over the hero's own turn. It used to survive the turn
// being taken by the clock, leaving a picker on screen with nothing left to confirm
// and the table hidden behind it.
func TestSyncState_ClosesTheSuitPickerWhenTheTurnIsLost(t *testing.T) {
	t.Parallel()
	engine, m := tableOnTurn(t)

	m.pickingSuit = true
	m.suitCursor = 2
	require.NoError(t, engine.SubmitAction(m.Bound.PlayerID(), logic.ActionDrawCard{}))
	m.syncState()

	require.False(t, m.Base.MyTurn, "drawing passes the turn on")
	assert.False(t, m.pickingSuit, "the picker cannot outlive the turn it belongs to")
}

func TestSyncState_KeepsTheSuitPickerWhileTheTurnIsStillYours(t *testing.T) {
	t.Parallel()
	_, m := tableOnTurn(t)

	m.pickingSuit = true
	m.syncState()

	assert.True(t, m.pickingSuit, "a refresh mid-turn must not close the picker")
}

// Mirrors the poker view's teardown test. Without it, a Close regression here parks
// a listener goroutine and holds a broadcaster slot for every disconnected player,
// and nothing in this package would notice.
func TestClose_ReleasesEngineSubscription(t *testing.T) {
	t.Parallel()

	players := []*game.Player{
		{ID: "1", UserID: 1, Name: "alice"},
		{ID: "2", UserID: 2, Name: "bob"},
	}
	engine := game.NewEngine(&logic.Rules{}, players, deck.StandardDeck())
	require.NoError(t, engine.Start())
	t.Cleanup(engine.Close)

	global := router.GlobalContext{User: &db.User{ID: 1, Username: "alice"}}
	m, ok := New(global, engine).(*Model)
	require.True(t, ok)
	require.Equal(t, 1, engine.Broadcaster().Len(), "the view subscribed on construction")

	// Park a listener exactly as the Bubble Tea runtime would. Init returns a batch
	// that also drives the animation tick, so take the event listener directly.
	// Built here because Close writes m.Events.
	listen := m.Listen()
	done := make(chan tea.Msg, 1)
	go func() { done <- listen() }()

	m.Close()
	assert.Zero(t, engine.Broadcaster().Len(), "Close returns the subscriber slot")

	select {
	case msg := <-done:
		assert.Nil(t, msg, "unsubscribing closes the channel so the listener returns")
	case <-time.After(2 * time.Second):
		t.Fatal("listener goroutine did not return after Close")
	}

	m.Close() // idempotent: session teardown may follow a view that already exited
	assert.Zero(t, engine.Broadcaster().Len())
}
