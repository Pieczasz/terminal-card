package game

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTurns_NextWrapsForward(t *testing.T) {
	t.Parallel()
	m := NewTurnManager(3)

	m.Next()
	assert.Equal(t, 1, m.Current())
	m.Next()
	assert.Equal(t, 2, m.Current())
	m.Next()
	assert.Equal(t, 0, m.Current(), "wraps back to first seat")
}

func TestTurns_ClampCurrent(t *testing.T) {
	t.Parallel()
	m := NewTurnManager(2)

	// A stale index past the end (e.g. a leftover OverrideNextTurn computed when
	// there were more players) must never survive as an out-of-range value.
	m.SetCurrent(5)
	m.clampCurrent()
	assert.GreaterOrEqual(t, m.Current(), 0)
	assert.Less(t, m.Current(), 2)

	m.SetCurrent(-1)
	m.clampCurrent()
	assert.GreaterOrEqual(t, m.Current(), 0)
	assert.Less(t, m.Current(), 2)
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
