// Package game contains game logic handling and initialization of a new game state,
// handling different rules, player seats, connections, state, and turns.
package game

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"sync"

	"github.com/Pieczasz/terminal-card/internal/broadcaster"
	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/player"
)

type Engine struct {
	mu          sync.Mutex
	state       *State
	turnManager *TurnManager
	broadcaster *broadcaster.Broadcaster[Event]
}

func NewEngine(rules Rules, players []*player.Player, cards []deck.Card) *Engine {
	return &Engine{
		state:       NewState(rules, players, cards),
		turnManager: NewTurnManager(len(players)),
		// Headroom above the player count for non-player subscribers (the lobby's
		// ranked-finalize watcher) and brief reconnect overlap; too small a cap
		// hands a closed channel to a real player, freezing their view.
		broadcaster: broadcaster.New[Event](len(players) + 8),
	}
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

// standingsLocked returns rules standings followed by LeftPlayers (the most
// recent leave-last place). Caller must hold e.mu and e.state.mu.
func (e *Engine) standingsLocked() []*player.Player {
	standings := e.state.Rules.Standings(e.state)
	out := make([]*player.Player, 0, len(standings)+len(e.state.LeftPlayers))
	out = append(out, standings...)
	for _, p := range slices.Backward(e.state.LeftPlayers) {
		out = append(out, p)
	}
	return out
}

// WithState allows thread-safe read access to the game state.
// The provided function is executed while holding the state lock.
// Prefer SnapshotFor / BoundEngine for TUI and untrusted callers.
func (e *Engine) WithState(fn func(state *State)) {
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
	fn(e.state)
}

// SnapshotFor returns public table state and hand sizes but no hand contents;
// a player reads their own cards via BoundEngine.Hand. The viewer parameter is
// reserved for future per-viewer redaction.
func (e *Engine) SnapshotFor(_ string) StateSnapshot {
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

func (e *Engine) SubmitAction(playerID string, action Action) error {
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

// Close releases engine broadcaster resources. Safe to call multiple times.
func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.broadcaster != nil {
		e.broadcaster.Close()
	}
}
