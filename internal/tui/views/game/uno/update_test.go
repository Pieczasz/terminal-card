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
)

func TestUpdate_Navigation(t *testing.T) {
	t.Parallel()
	m := Model{
		Base: gameview.BaseState{
			Hand: []deck.Card{
				{Rank: deck.Two, Suit: logic.ColorRed},
				{Rank: deck.Three, Suit: logic.ColorBlue},
				{Rank: deck.Four, Suit: logic.ColorGreen},
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
	engine := game.NewEngine(&logic.Rules{}, players, (&logic.Rules{}).InitialDeck())
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
	engine := game.NewEngine(&logic.Rules{}, players, (&logic.Rules{}).InitialDeck())
	require.NoError(t, engine.Start())
	t.Cleanup(engine.Close)

	global := router.GlobalContext{User: &db.User{ID: 1, Username: "alice"}}
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

// The colour glyphs sit at card-slot spacing, so they only make sense over a fan.
// A default 80x24 terminal is too short for one, and the hand falls back to the
// compact strip - a row drawn there lines up with nothing, which loses the one thing
// colour conveys in Uno.
func TestRenderHandColorRow_FollowsTheHandItSitsOver(t *testing.T) {
	t.Parallel()

	hand := make([]deck.Card, 7)
	for i := range hand {
		hand[i] = deck.Card{Rank: deck.Rank(logic.Zero + deck.Rank(i)), Suit: logic.ColorRed}
	}

	tests := []struct {
		name          string
		width, height int
		wantRow       bool
	}{
		{name: "a terminal tall enough to fan", width: 100, height: 40, wantRow: true},
		{name: "the default 80x24, which strips the hand", width: 80, height: 24, wantRow: false},
		{name: "the shortest admitted height", width: 80, height: 20, wantRow: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &Model{
				Global: router.GlobalContext{Width: tt.width, Height: tt.height},
				Base:   gameview.BaseState{Hand: hand},
			}

			handWidth := gameview.HandWidth(tt.width)
			handRows := gameview.HandRows(tt.height)
			row := m.renderHandColorRow(handWidth, handRows)

			fanned := gameview.FansHand(len(hand), handWidth, handRows)
			require.Equal(t, tt.wantRow, fanned, "test expectation must match the renderer's own choice")
			assert.Equal(t, tt.wantRow, row != "", "the colour row appears exactly when the hand fans")
		})
	}
}
