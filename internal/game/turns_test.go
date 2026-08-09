package game

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// State.CurrentTurn is the only cursor there is, so a removal has to reindex it in
// place: whoever was on turn stays on turn unless they are the one who left.
func TestEngine_RemovePlayer_ShiftsTheCursor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		current     int
		remove      string
		wantCurrent int
		wantOnTurn  string
	}{
		{name: "removing an earlier seat keeps the same player on turn", current: 2, remove: "p1", wantCurrent: 1, wantOnTurn: "p3"},
		{name: "removing a later seat leaves the cursor alone", current: 0, remove: "p3", wantCurrent: 0, wantOnTurn: "p1"},
		{name: "removing the seat on turn passes the turn on", current: 1, remove: "p2", wantCurrent: 1, wantOnTurn: "p3"},
		{name: "a trailing cursor wraps back to the first seat", current: 2, remove: "p3", wantCurrent: 0, wantOnTurn: "p1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			engine := newStartedEngine(t, "p1", "p2", "p3")
			engine.WithState(func(state *State) { state.CurrentTurn = tt.current })

			engine.RemovePlayer(tt.remove)

			engine.WithState(func(state *State) {
				assert.Equal(t, tt.wantCurrent, state.CurrentTurn)
			})
			assert.Equal(t, tt.wantOnTurn, engine.CurrentPlayerID())
		})
	}
}

func TestEngine_TurnAdvancesForwardAndWraps(t *testing.T) {
	t.Parallel()
	engine := newStartedEngine(t, "p1", "p2", "p3")
	engine.WithState(func(state *State) { state.CurrentTurn = 0 })

	for _, want := range []string{"p2", "p3", "p1"} {
		require.NoError(t, engine.SubmitAction(engine.CurrentPlayerID(), MockAction{name: "Move"}))
		assert.Equal(t, want, engine.CurrentPlayerID())
	}
}

// A cursor pointing at no seat must name nobody rather than panic: views read it
// while a leave is in flight.
func TestEngine_CurrentPlayerID_OutOfRangeCursorNamesNobody(t *testing.T) {
	t.Parallel()

	for _, current := range []int{-1, 7} {
		engine := newStartedEngine(t, "p1", "p2")
		engine.WithState(func(state *State) { state.CurrentTurn = current })
		assert.Empty(t, engine.CurrentPlayerID())
	}
}
