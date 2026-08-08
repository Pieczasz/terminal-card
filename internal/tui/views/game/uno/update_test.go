package uno

import (
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/uno"
	"github.com/Pieczasz/terminal-card/internal/player"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	gameview "github.com/Pieczasz/terminal-card/internal/tui/views/game"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUpdate_Navigation(t *testing.T) {
	t.Parallel()
	m := Model{
		baseState: gameview.BaseState{
			Hand: []deck.Card{
				{Rank: deck.Two, Suit: logic.ColorRed},
				{Rank: deck.Three, Suit: logic.ColorBlue},
				{Rank: deck.Four, Suit: logic.ColorGreen},
			},
		},
		selectedCardIdx: 0,
	}

	msg := tea.KeyPressMsg{Code: rune("l"[0]), Text: "l"}
	newM, _ := m.Update(msg)
	assert.Equal(t, 1, newM.(*Model).selectedCardIdx)

	msg = tea.KeyPressMsg{Code: rune("h"[0]), Text: "h"}
	newM, _ = newM.Update(msg)
	assert.Equal(t, 0, newM.(*Model).selectedCardIdx)
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

func TestClose_ReleasesEngineSubscription(t *testing.T) {
	t.Parallel()

	players := []*player.Player{
		{ID: "1", DatabaseUser: &db.User{Model: gorm.Model{ID: 1}, Username: "alice"}},
		{ID: "2", DatabaseUser: &db.User{Model: gorm.Model{ID: 2}, Username: "bob"}},
	}
	engine := game.NewEngine(&logic.Rules{}, players, logic.InitialDeck())
	require.NoError(t, engine.Start())
	t.Cleanup(engine.Close)

	global := router.GlobalContext{User: &db.User{Model: gorm.Model{ID: 1}, Username: "alice"}}
	m, ok := New(global, engine).(*Model)
	require.True(t, ok)
	require.Equal(t, 1, engine.Broadcaster().Len())

	listen := listenForEvents(m.events)
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
