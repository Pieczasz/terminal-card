package crazyeight

import (
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/tui/animation"

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

// The frame loop used to re-arm unconditionally, so every session re-rendered at
// 60 FPS forever for an animation that had already stopped moving.
func TestAnimation_StopsWhenTheSpringSettles(t *testing.T) {
	t.Parallel()
	m := &Model{
		selectionSpring: animation.DefaultSpring(),
		selectionLift:   0,
		animating:       true,
	}

	// settled must be tracked explicitly: `for frames = range budget` leaves frames at
	// budget-1 whether the loop broke early or ran out, so comparing it to the budget
	// proves nothing.
	const budget = 600
	var frames int
	var settled bool
	var cmd tea.Cmd = func() tea.Msg { return animation.FrameMsg(time.Now()) }
	for frames = range budget {
		msg := cmd()
		if _, isFrame := msg.(animation.FrameMsg); !isFrame {
			break
		}
		if _, cmd = m.Update(msg); cmd == nil {
			settled = true
			break
		}
	}

	require.True(t, settled, "the loop must stop on its own before the %d-frame budget", budget)
	assert.False(t, m.animating, "the model records that it stopped")
	assert.InDelta(t, selectionRest, m.selectionLift, selectionEpsilon, "it settles at the target")
	t.Logf("settled after %d frames (~%dms at 60 FPS)", frames, frames*1000/animation.FPS)
}

// Moving the selection has to start the loop again, or the lift animation would only
// ever play once per session.
func TestAnimation_SelectionChangeRestartsTheLoop(t *testing.T) {
	t.Parallel()
	m := &Model{
		selectionSpring: animation.DefaultSpring(),
		selectionLift:   selectionRest,
		animating:       false,
		baseState:       gameview.BaseState{Hand: make([]deck.Card, 3)},
	}

	_, cmd := m.Update(keyMsg("l"))

	require.NotNil(t, cmd, "moving the selection restarts the animation")
	assert.True(t, m.animating)
	assert.Zero(t, m.selectionLift, "the card drops before springing back up")
}

// A second selection change while the loop is already running must not start a
// second chain, which would double the frame rate and never merge back.
func TestAnimation_DoesNotStartASecondLoop(t *testing.T) {
	t.Parallel()
	m := &Model{
		selectionSpring: animation.DefaultSpring(),
		animating:       true, // already running
		baseState:       gameview.BaseState{Hand: make([]deck.Card, 3)},
	}

	_, cmd := m.Update(keyMsg("l"))

	assert.Nil(t, cmd, "no extra tick while the loop is already running")
	assert.True(t, m.animating)
}

// keyMsg builds the message Bubble Tea would deliver for a keystroke.
func keyMsg(key string) tea.KeyPressMsg {
	switch key {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	default:
		return tea.KeyPressMsg{Code: rune(key[0]), Text: key}
	}
}
