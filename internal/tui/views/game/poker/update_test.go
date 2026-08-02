package poker

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A disconnect never runs the view's esc/enter paths, so Close is the only thing
// that releases the engine subscription. If it regresses, the Len assertion fails
// and the parked listener below never returns, so the test times out.
func TestClose_ReleasesEngineSubscription(t *testing.T) {
	t.Parallel()
	engine, m := startedTable(t)
	broadcaster := engine.Broadcaster()

	require.Equal(t, 1, broadcaster.Len(), "the view subscribed on construction")

	// Park a listener on the channel exactly as the Bubble Tea runtime would. The
	// command is built here, on this goroutine, because Close writes m.events.
	listen := m.Init()
	done := make(chan tea.Msg, 1)
	go func() { done <- listen() }()

	m.Close()

	assert.Zero(t, broadcaster.Len(), "Close returns the subscriber slot")
	assert.Nil(t, <-done, "unsubscribing closes the channel so the listener returns")

	m.Close() // idempotent: the session teardown may run after a view already exited
	assert.Zero(t, broadcaster.Len())

	engine.Close()
}

// The router owns teardown, so a view swap must release the outgoing view too.
func TestClose_AfterEngineClosed(t *testing.T) {
	t.Parallel()
	engine, m := startedTable(t)
	engine.Close()

	m.Close()
	assert.Zero(t, engine.Broadcaster().Len())
}

// stepRaise works in uint, so decreasing below the step would wrap to a huge number.
// The guard is the whole reason the branch exists.
func TestStepRaise_ClampsWithinTheLegalBandAndNeverWraps(t *testing.T) {
	t.Parallel()
	_, m := startedTable(t)
	m.raising = true

	// Heads-up with DefaultStack=1000, SB=25, BB=50 the band is [100, 1000].
	const wantMin, wantMax = uint(100), uint(1000)

	m.raiseAmount = 0
	m.stepRaise(-1)
	assert.GreaterOrEqual(t, m.raiseAmount, wantMin, "decreasing from zero must not wrap")
	assert.LessOrEqual(t, m.raiseAmount, wantMax)

	for range 50 {
		m.stepRaise(-1)
		require.GreaterOrEqual(t, m.raiseAmount, wantMin, "repeated decrease stays legal")
	}

	for range 200 {
		m.stepRaise(+1)
		require.LessOrEqual(t, m.raiseAmount, wantMax, "repeated increase never exceeds the stack")
	}
	assert.Equal(t, wantMax, m.raiseAmount, "increasing eventually reaches the stack")
}

// Stepping while the raise prompt is closed must do nothing at all.
func TestStepRaise_IsANoOpWhenNotRaising(t *testing.T) {
	t.Parallel()
	_, m := startedTable(t)
	m.raising = false
	m.raiseAmount = 0

	m.stepRaise(+1)
	m.stepRaise(-1)

	assert.Zero(t, m.raiseAmount, "no adjustment outside the raise prompt")
}
