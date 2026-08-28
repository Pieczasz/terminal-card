package ginrummy

import (
	"testing"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/ginrummy"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"
	gameview "github.com/Pieczasz/terminal-card/internal/tui/views/game"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestView_ActionBarHints(t *testing.T) {
	t.Parallel()
	m := &Model{
		Global: router.GlobalContext{
			Theme:  styles.NewTheme(true),
			Width:  80,
			Height: 40,
		},
		Base: gameview.BaseState{
			Phase: game.Playing,
			Hand:  []deck.Card{{Rank: deck.Ace, Suit: deck.Spades}},
		},
		handPhase:  logic.AwaitingDraw,
		handNumber: 1,
		seatOrder:  []string{"1", "2"},
		seatNames:  map[string]string{"1": "alice", "2": "bob"},
	}

	out := m.View().Content
	require.NotEmpty(t, out)
	assert.Contains(t, out, "draw stock")

	m.handPhase = logic.AwaitingDiscard
	out = m.View().Content
	assert.Contains(t, out, "knock")
}

func TestView_HandOverWallBanner(t *testing.T) {
	t.Parallel()
	m := &Model{
		Global: router.GlobalContext{
			Theme:  styles.NewTheme(true),
			Width:  80,
			Height: 24,
		},
		Base:             gameview.BaseState{Phase: game.Playing},
		handPhase:        logic.HandOver,
		handComplete:     true,
		handNumber:       2,
		seatOrder:        []string{"1", "2"},
		seatNames:        map[string]string{"1": "alice", "2": "bob"},
		lastHandResult:   &logic.HandResult{Wall: true},
		cumulativeScores: map[string]int{"1": 10, "2": 5},
	}

	out := m.View().Content
	require.NotEmpty(t, out)
	assert.Contains(t, out, "HAND 2 COMPLETE")
	assert.Contains(t, out, "WALL")
}
