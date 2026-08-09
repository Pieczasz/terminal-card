package hearts

import (
	"testing"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/hearts"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"
	gameview "github.com/Pieczasz/terminal-card/internal/tui/views/game"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestView_HandOverShowsScores(t *testing.T) {
	t.Parallel()
	m := &Model{
		Session: gameview.Session{
			Global: router.GlobalContext{
				Theme:  styles.NewTheme(true),
				Width:  80,
				Height: 24,
			},
			Base: gameview.BaseState{Phase: game.Playing},
		},
		stage:        logic.StageHandOver,
		handComplete: true,
		handNumber:   2,
		seatOrder:    []string{"1", "2", "3", "4"},
		seatNames: map[string]string{
			"1": "alice", "2": "bob", "3": "carol", "4": "dave",
		},
		handPoints:       map[string]int{"1": 5, "2": 8, "3": 3, "4": 10},
		cumulativeScores: map[string]int{"1": 25, "2": 8, "3": 18, "4": 17},
	}

	out := m.View().Content
	require.NotEmpty(t, out)
	assert.Contains(t, out, "HAND 2 COMPLETE")
	assert.Contains(t, out, "alice")
	assert.Contains(t, out, "25")
}

func TestView_HeartsBrokenIndicator(t *testing.T) {
	t.Parallel()
	m := &Model{
		Session: gameview.Session{
			Global: router.GlobalContext{Theme: styles.NewTheme(true)},
		},
		heartsBroken: true,
		trickCards:   map[string]deck.Card{},
	}
	assert.Contains(t, m.renderHeartsBrokenIndicator(), "broken")
	m.heartsBroken = false
	assert.Contains(t, m.renderHeartsBrokenIndicator(), "not yet broken")
}
