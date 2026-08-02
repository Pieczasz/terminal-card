package poker

import (
	"testing"

	logic "github.com/Pieczasz/terminal-card/internal/game/poker"

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

// Chips are how a raise is built, so a keyed chip must land on top of an already
// legal amount and never push the raise outside the band.
func TestAddChip_StacksOntoTheOpenRaise(t *testing.T) {
	t.Parallel()
	_, m := startedTable(t)
	// The button is dealt at random, so the prompt is opened directly rather than
	// through beginRaise, which needs the hero to be on turn.
	m.raising = true
	m.raiseAmount = m.clampRaise(m.currentBet + m.minRaise)
	opening := m.raiseAmount

	m.addChip("3") // 25
	assert.Equal(t, opening+25, m.raiseAmount)

	m.addChip("1") // 100
	assert.Equal(t, opening+125, m.raiseAmount)

	for range 50 {
		m.addChip("1")
	}
	assert.Equal(t, m.streetBetMax(), m.raiseAmount, "chips stop at the player's stack")
}

func TestAddChip_IsANoOpWhenNotRaising(t *testing.T) {
	t.Parallel()
	_, m := startedTable(t)
	m.raising = false
	m.raiseAmount = 0

	m.addChip("1")

	assert.Zero(t, m.raiseAmount, "chips only move inside the raise prompt")
}

// Between hands the table waits on one player to deal. Enter must send that, not
// drop everyone back to the lobby the way the end of a match does.
func TestConfirm_DealsTheNextHandInsteadOfLeaving(t *testing.T) {
	t.Parallel()
	engine, m := startedTable(t)
	t.Cleanup(engine.Close)

	// Fold the hand out heads-up; the match still has hands left to play.
	require.NoError(t, engine.SubmitAction(engine.CurrentPlayerID(), logic.ActionFold{}))
	m.syncState()
	require.True(t, m.handDone)
	require.False(t, m.matchDone, "one hand is not the whole match")

	// The button lands on either seat, so both sides of the prompt get asserted.
	heroDeals := m.canDeal()
	_, cmd := m.confirm()
	assert.Nil(t, cmd, "enter between hands never navigates away")
	if !heroDeals {
		assert.True(t, m.handDone, "the hero cannot deal on another player's button")
		require.NoError(t, engine.SubmitAction(engine.CurrentPlayerID(), logic.ActionNextHand{}))
		m.syncState()
	}

	assert.False(t, m.handDone, "the next hand is under way")
	assert.Equal(t, 2, m.handNumber)
}

// A pot nobody contested is won face-down. The hand-over screen is shown between
// hands now, so a leaked hole card is a live read for the rest of the match.
func TestSyncState_UncontestedPotKeepsOpponentCardsHidden(t *testing.T) {
	t.Parallel()
	engine, m := startedTable(t)
	t.Cleanup(engine.Close)

	require.NoError(t, engine.SubmitAction(engine.CurrentPlayerID(), logic.ActionFold{}))
	m.syncState()
	require.True(t, m.handDone)

	for _, s := range m.seats {
		if s.IsHero {
			continue
		}
		assert.Empty(t, s.Hole, "%s never had to show", s.Name)
	}
}

// esc on the between-hands screen leaves the whole match, so the screen has to say
// so: it otherwise looks exactly like the end-of-game screen where esc was free.
func TestHandOverHint_SaysWhatEscCosts(t *testing.T) {
	t.Parallel()
	_, m := startedTable(t)
	m.handDone = true
	m.matchDone = false

	assert.Contains(t, m.handOverHint(), "forfeiting")

	m.matchDone = true
	assert.NotContains(t, m.handOverHint(), "forfeiting", "the match is over, esc costs nothing")
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
