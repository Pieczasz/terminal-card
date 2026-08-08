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

	// A stale index past the end (e.g., a leftover OverrideNextTurn computed when
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

// RemovePlayer's cursor shift is what stops a turn being handed to the wrong seat when an
// earlier player leaves.
func TestTurnManager_RemovePlayer_ShiftsTheCursor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		count       int
		current     int
		remove      int
		wantCurrent int
		wantCount   int
	}{
		{name: "removing an earlier seat shifts the cursor down", count: 3, current: 2, remove: 0, wantCurrent: 1, wantCount: 2},
		{name: "the shift moves down, not up", count: 4, current: 3, remove: 0, wantCurrent: 2, wantCount: 3},
		{name: "removing a later seat leaves the cursor alone", count: 3, current: 0, remove: 2, wantCurrent: 0, wantCount: 2},
		{name: "removing the seat on turn keeps the index", count: 3, current: 1, remove: 1, wantCurrent: 1, wantCount: 2},
		{name: "a trailing cursor is clamped back in range", count: 2, current: 1, remove: 1, wantCurrent: 0, wantCount: 1},
		{name: "emptying the table resets to a single seat", count: 1, current: 0, remove: 0, wantCurrent: 0, wantCount: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tm := NewTurnManager(tt.count)
			tm.SetCurrent(tt.current)

			tm.RemovePlayer(tt.remove)

			assert.Equal(t, tt.wantCurrent, tm.Current(), "cursor")
			assert.Equal(t, tt.wantCount, tm.playerCount, "seat count")
			assert.GreaterOrEqual(t, tm.Current(), 0, "the cursor never goes negative")
			assert.Less(t, tm.Current(), tm.playerCount, "the cursor always addresses a real seat")
		})
	}
}

// Next and clampCurrent must survive a zero seat count rather than dividing by zero.
func TestTurnManager_EmptyTableIsSafe(t *testing.T) {
	t.Parallel()
	tm := NewTurnManager(0)

	tm.SetCurrent(7)
	tm.Next()
	assert.Equal(t, 7, tm.Current(), "Next is a no-op with no seats")

	tm.clampCurrent()
	assert.Equal(t, 0, tm.Current(), "clamping with no seats resets to zero")
}
