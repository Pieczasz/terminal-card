package game

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTurns_Reverse(t *testing.T) {
	t.Parallel()
	m := NewTurnManager(3)

	m.Reverse()

	m.Next()
	assert.Equal(t, 2, m.Current())

	m.Next()
	assert.Equal(t, 1, m.Current())
}

func TestTurns_RemovePlayer(t *testing.T) {
	t.Parallel()
	m := NewTurnManager(3)

	m.RemovePlayer(1)

	m.Next()
	assert.Equal(t, 1, m.Current())

	m.RemovePlayer(1)

	m.Next()
	assert.Equal(t, 0, m.Current())
}
