package ginrummy

import (
	"testing"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/ginrummy"
	gameview "github.com/Pieczasz/terminal-card/internal/tui/views/game"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
)

func TestUpdate_Navigation(t *testing.T) {
	t.Parallel()
	m := Model{
		baseState: gameview.BaseState{
			Hand: []deck.Card{
				{Rank: deck.Two, Suit: deck.Hearts},
				{Rank: deck.Three, Suit: deck.Clubs},
				{Rank: deck.Four, Suit: deck.Spades},
			},
			MyTurn: true,
			Phase:  game.Playing,
		},
		handPhase:       logic.AwaitingDiscard,
		selectedCardIdx: 0,
	}

	msg := tea.KeyPressMsg{Code: rune("l"[0]), Text: "l"}
	newM, _ := m.Update(msg)
	assert.Equal(t, 1, newM.(*Model).selectedCardIdx)

	msg = tea.KeyPressMsg{Code: rune("h"[0]), Text: "h"}
	newM, _ = newM.Update(msg)
	assert.Equal(t, 0, newM.(*Model).selectedCardIdx)
}

func TestUpdate_NumberSelection(t *testing.T) {
	t.Parallel()
	m := Model{
		baseState: gameview.BaseState{
			Hand: make([]deck.Card, 10),
		},
		selectedCardIdx: 0,
	}
	msg := tea.KeyPressMsg{Code: rune("5"[0]), Text: "5"}
	newM, _ := m.Update(msg)
	assert.Equal(t, 5, newM.(*Model).selectedCardIdx)
}
