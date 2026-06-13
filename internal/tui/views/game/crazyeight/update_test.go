package crazyeight

import (
	"testing"

	"terminalcard/internal/deck"
	gameview "terminalcard/internal/tui/views/game"

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
	assert.Equal(t, 1, newM.(Model).selectedCardIdx)

	// Left
	msg = tea.KeyPressMsg{Code: rune("h"[0]), Text: "h"}
	newM, _ = newM.Update(msg)
	assert.Equal(t, 0, newM.(Model).selectedCardIdx)
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
	assert.Equal(t, 1, newM.(Model).suitCursor)

	// Down (adds 2 to cursor)
	msg = tea.KeyPressMsg{Code: rune("j"[0]), Text: "j"}
	newM, _ = newM.Update(msg)
	assert.Equal(t, 3, newM.(Model).suitCursor)
}
