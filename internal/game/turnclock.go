package game

import (
	"log/slog"
	"time"
)

const (
	DefaultTurnTimeout = 30 * time.Second
	MaxMissedTurns     = 3
)

func (e *Engine) armTurnTimerLocked() {
	e.stopTurnTimerLocked()

	if e.turnTimeout <= 0 || e.state.Phase != Playing || len(e.state.Players) == 0 {
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
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
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
	if err := e.submitAction(playerID, action, false); err != nil {
		slog.Warn("auto-play for an expired turn was refused",
			"error", err, "player_id", playerID, "action", action.Name())
		e.rearmTurnTimer()
	}
}

func (e *Engine) resolveTurnTimeout(seq uint64) (playerID string, action Action, takeSeat bool) {
	e.mu.Lock()
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
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

	handler, ok := e.state.Rules.(TurnTimeoutHandler)
	if !ok {
		return "", nil, false
	}
	safe := handler.TimeoutAction(e.state)
	if safe == nil {
		// The rules have no safe move here, so idling cannot be absorbed by playing
		// on: the seat goes rather than the table waiting. Counting it as a full set
		// of misses is what makes that decision re-checkable in removeIfStillIdle.
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
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
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
	e.broadcaster.Broadcast(Event{Type: EventPlayerIdle, PlayerID: playerID})
	e.removePlayerLocked(playerID)
}
