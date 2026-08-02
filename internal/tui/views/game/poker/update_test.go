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
