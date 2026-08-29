package game

import (
	"errors"
	"log/slog"
	"time"
)

const (
	DefaultTurnTimeout = 30 * time.Second
	MaxMissedTurns     = 3
)

func (e *Engine) armTurnTimerLocked() {
	e.stopTurnTimerLocked()

	if e.closed || e.turnTimeout <= 0 || e.state.Phase != Playing || len(e.state.Players) == 0 {
		return
	}
	if _, ok := e.state.Rules.(TurnTimeoutHandler); !ok {
		return
	}

	timeout := e.turnTimeout
	if h, ok := e.state.Rules.(TurnDurationHandler); ok {
		if override := h.TurnTimeout(e.state); override > 0 {
			timeout = override
		}
	}

	seq := e.turnSeq
	e.turnDeadline = time.Now().Add(timeout)
	e.turnTimer = time.AfterFunc(timeout, func() { e.onTurnTimeout(seq) })
}

func (e *Engine) stopTurnTimerLocked() {
	e.turnSeq++
	if e.turnTimer != nil {
		e.turnTimer.Stop()
		e.turnTimer = nil
	}
	e.turnDeadline = time.Time{}
}

func (e *Engine) rearmTurnTimer() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.armTurnTimerLocked()
}

func (e *Engine) TurnDeadline() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.turnDeadline
}

func (e *Engine) MissedTurns(playerID string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.missedTurns[playerID]
}

func (e *Engine) onTurnTimeout(seq uint64) {
	playerID, action, takeSeat := e.resolveTurnTimeout(seq)
	if playerID == "" {
		return
	}

	if takeSeat {
		e.removeIfStillIdle(seq, playerID)
		return
	}

	e.broadcaster.Broadcast(Event{Type: EventTurnTimedOut, PlayerID: playerID})
	err := e.submitTimedOutAction(playerID, action, seq)
	switch {
	case err == nil, errors.Is(err, errStaleTurn):
		// The player acted for themselves while the lock was dropped: their action
		// armed a fresh clock, so re-arming would hand the next player a double turn.
		return
	default:
		// TimeoutAction returned a move ValidateAction refuses: a rules bug. The seat
		// is taken on the next expiry instead.
		slog.Warn("auto-play for an expired turn was refused",
			"error", err, "player_id", playerID, "action", action.Name())
		e.rearmTurnTimer()
	}
}

func (e *Engine) resolveTurnTimeout(seq uint64) (playerID string, action Action, takeSeat bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if seq != e.turnSeq || e.state.Phase != Playing || len(e.state.Players) == 0 {
		return "", nil, false
	}

	current := e.currentPlayerLocked()
	if current == nil {
		return "", nil, false
	}

	e.missedTurns[current.ID]++
	if e.missedTurns[current.ID] >= MaxMissedTurns {
		return current.ID, nil, true
	}

	// armTurnTimerLocked never arms without a TurnTimeoutHandler, and Rules is set once
	// at construction, so this cannot fail.
	safe := e.state.Rules.(TurnTimeoutHandler).TimeoutAction(e.state)
	if safe == nil {
		// No safe move: the seat goes rather than the table waiting. A full set of
		// misses is what makes that re-checkable in removeIfStillIdle. Reaching this in
		// real play is a rules bug, hence the log.
		slog.Warn("rules returned no safe timeout move; taking the seat",
			"player_id", current.ID, "phase", e.state.Phase)
		e.missedTurns[current.ID] = MaxMissedTurns
		return current.ID, nil, true
	}
	return current.ID, safe, false
}

// removeIfStillIdle re-checks the idle decision and removes under one lock hold.
// resolveTurnTimeout had to drop the locks before calling here; without this, a
// player who SubmitAction'd in that window would still be kicked.
func (e *Engine) removeIfStillIdle(seq uint64, playerID string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if seq != e.turnSeq {
		// The cursor moved on, so a newer timer already owns the turn.
		return
	}
	if e.missedTurns[playerID] < MaxMissedTurns {
		// They acted inside the window we had to drop the locks for. The timer that
		// brought us here has already fired, and a rejected action returns without
		// settling the cursor, so nothing else will re-arm: do it here or the table
		// sits on this seat with a dead clock forever.
		e.armTurnTimerLocked()
		return
	}
	// EventPlayerIdle ends the player's ssh session through the view, so this is the
	// only server-side record that a seat was taken for idling.
	slog.Info("removing idle player",
		"player_id", playerID, "missed_turns", e.missedTurns[playerID])
	e.broadcaster.Broadcast(Event{Type: EventPlayerIdle, PlayerID: playerID})
	e.removePlayerLocked(playerID)
}
