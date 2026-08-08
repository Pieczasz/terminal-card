package crazyeight

import (
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/crazyeight"
	"github.com/Pieczasz/terminal-card/internal/player"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/Pieczasz/terminal-card/internal/deck"
	gameview "github.com/Pieczasz/terminal-card/internal/tui/views/game"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
)

func TestUpdate_Navigation(t *testing.T) {
	t.Parallel()
	m := Model{
		baseState: gameview.BaseState{
			Hand: []deck.Card{
				{Rank: deck.Two, Suit: deck.Spades},
				{Rank: deck.Three, Suit: deck.Hearts},
				{Rank: deck.Four, Suit: deck.Clubs},
			},
		},
		selectedCardIdx: 0,
	}

	// Right
	msg := tea.KeyPressMsg{Code: rune("l"[0]), Text: "l"}
	newM, _ := m.Update(msg)
	assert.Equal(t, 1, newM.(*Model).selectedCardIdx)

	// Left
	msg = tea.KeyPressMsg{Code: rune("h"[0]), Text: "h"}
	newM, _ = newM.Update(msg)
	assert.Equal(t, 0, newM.(*Model).selectedCardIdx)
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

// Mirrors the poker view's teardown test. Without it, a Close regression here parks
// a listener goroutine and holds a broadcaster slot for every disconnected player,
// and nothing in this package would notice.
func TestClose_ReleasesEngineSubscription(t *testing.T) {
	t.Parallel()

	players := []*player.Player{
		{ID: "1", DatabaseUser: &db.User{Model: gorm.Model{ID: 1}, Username: "alice"}},
		{ID: "2", DatabaseUser: &db.User{Model: gorm.Model{ID: 2}, Username: "bob"}},
	}
	engine := game.NewEngine(&logic.Rules{}, players, deck.StandardDeck())
	require.NoError(t, engine.Start())
	t.Cleanup(engine.Close)

	global := router.GlobalContext{User: &db.User{Model: gorm.Model{ID: 1}, Username: "alice"}}
	m, ok := New(global, engine).(*Model)
	require.True(t, ok)
	require.Equal(t, 1, engine.Broadcaster().Len(), "the view subscribed on construction")

	// Park a listener exactly as the Bubble Tea runtime would. Init returns a batch
	// that also drives the animation tick, so take the event listener directly.
	// Built here because Close writes m.events.
	listen := listenForEvents(m.events)
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
