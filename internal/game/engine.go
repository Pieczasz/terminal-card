// Package game contains game logic handling and initialization of a new game state,
// handling different rules, player seats, connections, state, and turns.
package game

import (
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"slices"
	"sync"
	"time"

	"github.com/Pieczasz/terminal-card/internal/broadcaster"
	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/player"
)

const (
	// DefaultTurnTimeout is how long a player has to act before the engine plays
	// their safest move for them. One idle player must not be able to hold a table
	// hostage, and a table that stalls is indistinguishable to everyone else from a
	// server that has hung.
	DefaultTurnTimeout = 30 * time.Second

	// MaxMissedTurns is how many turns in a row a player may let expire before they
	// lose their seat. Their own action resets the count, so this only ever fires on
	// somebody who has stopped playing entirely.
	MaxMissedTurns = 3
)

type Engine struct {
	mu          sync.Mutex
	state       *State
	turnManager *TurnManager
	broadcaster *broadcaster.Broadcaster[Event]

	turnTimeout time.Duration
	// turnSeq increments whenever the cursor settles. A timer captures it when armed
	// and abandons its work if it no longer matches, so a player who acted in the
	// moment before their clock expired is never charged for it.
	turnSeq      uint64
	turnTimer    *time.Timer
	turnDeadline time.Time
	// missedTurns counts consecutive expiries per player, cleared by any action they
	// submit themselves.
	missedTurns map[string]int
}

// EngineOption adjusts an Engine at construction.
type EngineOption func(*Engine)

// WithTurnTimeout overrides how long a player has to act. A non-positive duration
// disables the clock entirely. Tests use it because they cannot wait out the real one.
func WithTurnTimeout(d time.Duration) EngineOption {
	return func(e *Engine) {
		e.turnTimeout = d
	}
}

func NewEngine(rules Rules, players []*player.Player, cards []deck.Card, opts ...EngineOption) *Engine {
	e := &Engine{
		state:       NewState(rules, players, cards),
		turnManager: NewTurnManager(len(players)),
		// Headroom above the player count for non-player subscribers (the lobby's
		// ranked-finalize watcher) and brief reconnect overlap; too small a cap
		// hands a closed channel to a real player, freezing their view.
		broadcaster: broadcaster.New[Event](len(players) + 8),
		turnTimeout: DefaultTurnTimeout,
		missedTurns: make(map[string]int, len(players)),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// CurrentPlayerID returns the ID of the player whose turn it is.
func (e *Engine) CurrentPlayerID() string {
	e.mu.Lock()
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
	defer e.mu.Unlock()
	return e.currentPlayerLocked().ID
}

func (e *Engine) currentPlayerLocked() *player.Player {
	return e.state.Players[e.turnManager.Current()]
}

// applyNextTurnLocked settles the turn cursor, keeping turnManager and
// state.CurrentTurn in agreement: a rules OverrideNextTurn wins, else advance
// steps forward, else a rules-set CurrentTurn is honored. The result is always
// clamped so a stale override cannot index out of range. Holds both locks.
func (e *Engine) applyNextTurnLocked(advance bool) {
	switch {
	case e.state.OverrideNextTurn != nil:
		e.turnManager.SetCurrent(*e.state.OverrideNextTurn)
		e.state.OverrideNextTurn = nil
	case advance:
		e.turnManager.Next()
	default:
		e.turnManager.SetCurrent(e.state.CurrentTurn)
	}
	e.turnManager.clampCurrent()
	e.state.CurrentTurn = e.turnManager.Current()
	e.armTurnTimerLocked()
}

// armTurnTimerLocked restarts the acting player's clock. Bumping turnSeq first
// invalidates any timer still in flight, which is what makes this safe to call on
// every cursor change. Caller must hold e.mu and e.state.mu.
func (e *Engine) armTurnTimerLocked() {
	e.stopTurnTimerLocked()

	if e.turnTimeout <= 0 || e.state.Phase != Playing || len(e.state.Players) == 0 {
		return
	}
	// Rules opt in by supplying a safe move. Without one there is nothing to play on
	// an absent player's behalf, so they get no clock rather than a silent removal.
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

// stopTurnTimerLocked cancels the running clock and invalidates any timer that has
// already fired but not yet taken the lock. Caller must hold e.mu and e.state.mu.
func (e *Engine) stopTurnTimerLocked() {
	e.turnSeq++
	if e.turnTimer != nil {
		e.turnTimer.Stop()
		e.turnTimer = nil
	}
	e.turnDeadline = time.Time{}
}

// TurnDeadline is when the acting player's clock expires. A zero time means no clock
// is running, either because the game is not in play or the rules have no safe move.
func (e *Engine) TurnDeadline() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.turnDeadline
}

// MissedTurns is how many turns in a row playerID has let expire.
func (e *Engine) MissedTurns(playerID string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.missedTurns[playerID]
}

// onTurnTimeout plays for a player whose clock ran out, and takes their seat once
// they have let MaxMissedTurns expire in a row.
//
// It runs on a timer goroutine, so it acquires the locks itself and re-checks seq
// before doing anything: by the time it gets in, the player may already have acted.
func (e *Engine) onTurnTimeout(seq uint64) {
	playerID, action, takeSeat := e.resolveTurnTimeout(seq)
	if playerID == "" {
		return
	}

	if takeSeat {
		// Broadcast before removing so the idle player's own session sees why it is
		// about to end, and the table learns who stopped playing.
		e.broadcaster.Broadcast(Event{Type: EventPlayerIdle, PlayerID: playerID})
		e.RemovePlayer(playerID)
		return
	}

	e.broadcaster.Broadcast(Event{Type: EventTurnTimedOut, PlayerID: playerID})
	if err := e.submitAction(playerID, action, false); err != nil {
		// Either the turn moved on between releasing the lock and here, or the rules
		// refused their own safe move. Re-arm so the table cannot stall on it: the
		// miss is already counted, so a seat that keeps failing still gets taken.
		slog.Warn("auto-play for an expired turn was refused",
			"error", err, "player_id", playerID, "action", action.Name())
		e.rearmTurnTimer()
	}
}

// resolveTurnTimeout decides, under both locks, what an expired clock means: who it
// belongs to, what to play for them, and whether this is the miss that costs them
// their seat. An empty playerID means the timeout is stale and there is nothing to do.
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
		// on: the seat goes rather than the table waiting.
		return current.ID, nil, true
	}
	return current.ID, safe, false
}

func (e *Engine) rearmTurnTimer() {
	e.mu.Lock()
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
	defer e.mu.Unlock()
	e.armTurnTimerLocked()
}

func (e *Engine) Broadcaster() *broadcaster.Broadcaster[Event] {
	return e.broadcaster
}

// StandingsIDs returns standing player IDs from first to last, including players who left.
func (e *Engine) StandingsIDs() []string {
	e.mu.Lock()
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
	defer e.mu.Unlock()

	standings := e.standingsLocked()
	ids := make([]string, 0, len(standings))
	for _, p := range standings {
		if p != nil {
			ids = append(ids, p.ID)
		}
	}
	return ids
}

// Standings return players ordered from first to last place.
// Callers must treat returned pointers as read-only snapshots of identity; card
// slices may still be shared - prefer WithState for hand inspection.
func (e *Engine) Standings() []*player.Player {
	e.mu.Lock()
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
	defer e.mu.Unlock()
	return e.standingsLocked()
}

// standingsLocked returns rules standings followed by any LeftPlayers the rules
// did not place themselves (the most recent leave taking the higher spot). Rules
// that can rank a departed player on what they actually did - poker ranks them on
// the chips they walked out with - place them, and are not second-guessed here.
// Caller must hold e.mu and e.state.mu.
func (e *Engine) standingsLocked() []*player.Player {
	standings := e.state.Rules.Standings(e.state)

	placed := make(map[string]bool, len(standings))
	for _, p := range standings {
		if p != nil {
			placed[p.ID] = true
		}
	}

	out := make([]*player.Player, 0, len(standings)+len(e.state.LeftPlayers))
	out = append(out, standings...)
	for _, p := range slices.Backward(e.state.LeftPlayers) {
		if p != nil && !placed[p.ID] {
			out = append(out, p)
		}
	}
	return out
}

// WithState allows thread-safe read access to the game state.
// The provided function is executed while holding the state lock.
// Prefer Snapshot / BoundEngine for TUI and untrusted callers.
func (e *Engine) WithState(fn func(state *State)) {
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
	fn(e.state)
}

// Snapshot returns public table state and hand sizes but no hand contents; a player
// reads their own cards via BoundEngine.Hand. Nothing here is viewer-specific, so
// there is no viewer parameter to get wrong - redaction lives in BoundEngine.Hand
// and in each view's own seat builder.
func (e *Engine) Snapshot() StateSnapshot {
	var snap StateSnapshot
	e.WithState(func(state *State) {
		snap.Phase = state.Phase
		if state.Deck != nil {
			snap.DeckSize = state.Deck.Size()
		}
		if state.Discard != nil {
			if top, ok := state.Discard.Peek(); ok {
				snap.TopDiscard = top
			}
		}
		if state.Winner != nil {
			snap.Winner = state.Winner.Username()
		}
		snap.Players = make([]PlayerSnapshot, 0, len(state.Players))
		if state.CurrentTurn >= 0 && state.CurrentTurn < len(state.Players) {
			snap.CurrentPlayer = state.Players[state.CurrentTurn].Username()
		}
		for _, p := range state.Players {
			if p == nil {
				continue
			}
			snap.Players = append(snap.Players, PlayerSnapshot{
				ID:       p.ID,
				Username: p.Username(),
				HandSize: len(p.Cards),
			})
		}
	})
	return snap
}

func (e *Engine) Start() error {
	e.mu.Lock()
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
	defer e.mu.Unlock()
	e.state.Phase = Dealing

	if len(e.state.Players) == 0 {
		return errors.New("cannot start game with no players")
	}

	if err := e.state.Deck.Shuffle(); err != nil {
		return fmt.Errorf("shuffle deck: %w", err)
	}

	for playerIdx := range e.state.Players {
		cards, ok := e.state.Deck.DrawNCards(e.state.Rules.InitialDealCount())
		if !ok {
			return errors.New("insufficient number of cards to deal for all players")
		}
		e.state.Players[playerIdx].Cards = cards
	}

	e.state.Phase = Playing
	startIdx, err := cryptoIntN(len(e.state.Players))
	if err != nil {
		return fmt.Errorf("selecting first player: %w", err)
	}
	e.turnManager.SetCurrent(startIdx)
	e.state.CurrentTurn = e.turnManager.Current()

	if err := e.state.Rules.OnGameStart(e.state); err != nil {
		return fmt.Errorf("failed to setup game: %w", err)
	}
	e.applyNextTurnLocked(false)

	e.broadcaster.Broadcast(Event{
		Type: EventGameStarted,
	})

	return nil
}

func cryptoIntN(n int) (int, error) {
	if n <= 0 {
		return 0, errors.New("n must be positive")
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, fmt.Errorf("crypto/rand: %w", err)
	}
	return int(v.Int64()), nil
}

// SubmitAction applies action on behalf of playerID, who must be the one on turn.
// A player acting for themselves clears their missed-turn count.
func (e *Engine) SubmitAction(playerID string, action Action) error {
	return e.submitAction(playerID, action, true)
}

// submitAction is SubmitAction with control over the missed-turn count. The engine
// passes false when playing for an absent player: that timeout has already been
// counted, and letting the auto-play clear it would mean somebody who never comes
// back is never removed.
func (e *Engine) submitAction(playerID string, action Action, playerPresent bool) error {
	e.mu.Lock()
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
	defer e.mu.Unlock()

	if e.state.Phase != Playing {
		return errors.New("game not in playing phase")
	}

	currentPlayer := e.currentPlayerLocked()
	if currentPlayer.ID != playerID {
		return errors.New("wait for your turn to perform an action")
	}

	// Cleared before validation: a player sending a move the rules reject is still
	// sitting at the keyboard, and idling is what this counter is for.
	if playerPresent {
		delete(e.missedTurns, playerID)
	}

	if err := e.state.Rules.ValidateAction(e.state, action); err != nil {
		return fmt.Errorf("you can't perform that action: %w", err)
	}

	// AfterAction is where rules advance their own state machine (poker settles
	// bets, deals the next street and picks the next actor), so it runs before any
	// broadcast and clients never observe a half-applied move. A failure here means
	// the rules could not reach a consistent state, so the game is finished rather
	// than played on; anything checkable up front belongs in ValidateAction.
	e.state.Rules.ApplyAction(e.state, action)

	if err := e.state.Rules.AfterAction(e.state, action); err != nil {
		// The game is over either way, so it ends the same way a win does: without
		// the broadcast every other client sits on a frame that will never update,
		// and the lobby never records the match.
		e.finishGameLocked(currentPlayer)
		return fmt.Errorf("post-action rules failed: %w", err)
	}

	e.broadcaster.Broadcast(Event{
		Type:     EventActionApplied,
		PlayerID: playerID,
		Action:   action,
	})

	if e.state.Rules.CheckWinCondition(e.state) {
		e.finishGameLocked(currentPlayer)
		return nil
	}

	e.applyNextTurnLocked(true)

	e.broadcaster.Broadcast(Event{
		Type: EventTurnAdvanced,
	})

	return nil
}

// finishGameLocked settles the winner from the rules standings and announces the
// end of the game. fallback names the winner when the rules rank nobody. Caller
// must hold e.mu and e.state.mu.
func (e *Engine) finishGameLocked(fallback *player.Player) {
	e.state.Phase = Finished
	// Nobody is on turn any more, and a clock left running would auto-play into a
	// finished game.
	e.stopTurnTimerLocked()

	standings := e.state.Rules.Standings(e.state)
	switch {
	case len(standings) > 0:
		e.state.Winner = standings[0]
	case fallback != nil:
		e.state.Winner = fallback
	case len(e.state.Players) > 0:
		e.state.Winner = e.state.Players[0]
	}

	winnerID := ""
	if e.state.Winner != nil {
		winnerID = e.state.Winner.ID
	}
	e.broadcaster.Broadcast(Event{
		Type:     EventGameEnded,
		PlayerID: winnerID,
	})
}

func (e *Engine) IsFinished() bool {
	e.mu.Lock()
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
	defer e.mu.Unlock()
	return e.state.Phase == Finished
}

func (e *Engine) RemovePlayer(playerID string) {
	e.mu.Lock()
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
	defer e.mu.Unlock()

	if e.state.Phase == Finished {
		return
	}

	playerIndex := -1
	for i, p := range e.state.Players {
		if p.ID == playerID {
			playerIndex = i
			break
		}
	}

	if playerIndex == -1 {
		return
	}

	if h, ok := e.state.Rules.(PlayerLeaveHandler); ok {
		h.OnPlayerLeave(e.state, playerID)
	}

	removedPlayer := e.state.Players[playerIndex]
	e.state.LeftPlayers = append(e.state.LeftPlayers, removedPlayer)

	e.state.Players = slices.Delete(e.state.Players, playerIndex, playerIndex+1)

	e.turnManager.RemovePlayer(playerIndex)
	e.state.CurrentTurn = e.turnManager.Current()

	if h, ok := e.state.Rules.(PlayerLeaveHandler); ok {
		h.AfterPlayerRemoved(e.state, playerIndex)
	}

	if e.state.Rules.CheckWinCondition(e.state) {
		e.finishGameLocked(nil)
		return
	}

	if len(e.state.Players) == 1 {
		e.state.Phase = Finished
		e.stopTurnTimerLocked()
		e.state.Winner = e.state.Players[0]
		e.broadcaster.Broadcast(Event{
			Type:     EventGameEnded,
			PlayerID: e.state.Winner.ID,
		})
		return
	}

	e.applyNextTurnLocked(false)

	e.broadcaster.Broadcast(Event{
		Type: EventTurnAdvanced,
	})
}

// Close stops the turn clock and releases engine broadcaster resources. Safe to call
// multiple times.
func (e *Engine) Close() {
	e.mu.Lock()
	e.state.mu.Lock()
	e.stopTurnTimerLocked()
	e.state.mu.Unlock()
	defer e.mu.Unlock()
	e.broadcaster.Close()
}
