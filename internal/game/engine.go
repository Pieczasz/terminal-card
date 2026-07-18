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

	"terminalcard/internal/broadcaster"
	"terminalcard/internal/deck"
	"terminalcard/internal/player"
)

type Engine struct {
	mu          sync.Mutex
	state       *State
	turnManager *TurnManager
	broadcaster *broadcaster.Broadcaster[Event]
}

func NewGameEngine(rules Rules, players []*player.Player, cards []deck.Card) *Engine {
	return &Engine{
		state:       NewState(rules, players, cards),
		turnManager: NewTurnManager(len(players)),
		broadcaster: broadcaster.New[Event](len(players)),
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

func (e *Engine) Broadcaster() *broadcaster.Broadcaster[Event] {
	return e.broadcaster
}

// StandingsIDs returns standing player IDs from first to last, including players who left.
func (e *Engine) StandingsIDs() []string {
	e.mu.Lock()
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
	defer e.mu.Unlock()

	standings := e.state.Rules.GetStandings(e.state)
	ids := make([]string, 0, len(standings)+len(e.state.LeftPlayers))
	for _, p := range standings {
		if p != nil {
			ids = append(ids, p.ID)
		}
	}
	for i := len(e.state.LeftPlayers) - 1; i >= 0; i-- {
		if e.state.LeftPlayers[i] != nil {
			ids = append(ids, e.state.LeftPlayers[i].ID)
		}
	}
	return ids
}

// Standings returns players ordered from first to last place.
// Callers must treat returned pointers as read-only snapshots of identity; card
// slices may still be shared — prefer WithState for hand inspection.
func (e *Engine) Standings() []*player.Player {
	e.mu.Lock()
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
	defer e.mu.Unlock()

	standings := e.state.Rules.GetStandings(e.state)
	out := make([]*player.Player, 0, len(standings)+len(e.state.LeftPlayers))
	out = append(out, standings...)
	for i := len(e.state.LeftPlayers) - 1; i >= 0; i-- {
		out = append(out, e.state.LeftPlayers[i])
	}
	return out
}

// WithState allows thread-safe read access to the game state.
// The provided function is executed while holding the state lock.
func (e *Engine) WithState(fn func(state *State)) {
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
	fn(e.state)
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

	e.broadcaster.Broadcast(Event{
		Sequence: 0,
		Type:     EventGameStarted,
	})

	return nil
}

func cryptoIntN(n int) (int, error) {
	if n <= 0 {
		return 0, errors.New("n must be positive")
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, err
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

	if err := e.state.Rules.PreActionCondition(e.state, action); err != nil {
		return fmt.Errorf("you can't perform that action: %w", err)
	}

	// Apply only after pre-conditions pass. Post-conditions run before any broadcast
	// so clients never observe a rejected mutation. On post-condition failure the
	// game is finished to avoid continued play on inconsistent state; rules that can
	// fully validate beforehand should do so in PreActionCondition.
	e.state.Rules.ApplyAction(e.state, action)

	if err := e.state.Rules.PostActionCondition(e.state, action); err != nil {
		e.state.Phase = Finished
		return fmt.Errorf("post condition doesn't hold: %w", err)
	}

	e.broadcaster.Broadcast(Event{
		Sequence: int64(e.turnManager.Current()),
		Type:     EventActionApplied,
		PlayerID: playerID,
		Action:   action,
	})

	if e.state.Rules.CheckWinCondition(e.state) {
		e.state.Phase = Finished
		standings := e.state.Rules.GetStandings(e.state)
		if len(standings) > 0 {
			e.state.Winner = standings[0]
		} else {
			e.state.Winner = currentPlayer
		}
		e.broadcaster.Broadcast(Event{
			Type:     EventGameEnded,
			PlayerID: e.state.Winner.ID,
		})
		return nil
	}

	if e.state.OverrideNextTurn != nil {
		e.turnManager.SetCurrent(*e.state.OverrideNextTurn)
		e.state.OverrideNextTurn = nil
	} else {
		e.turnManager.Next()
	}
	e.state.CurrentTurn = e.turnManager.Current()

	e.broadcaster.Broadcast(Event{
		Sequence: int64(e.turnManager.Current()),
		Type:     EventTurnAdvanced,
	})

	return nil
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

	removedPlayer := e.state.Players[playerIndex]
	e.state.LeftPlayers = append(e.state.LeftPlayers, removedPlayer)

	e.state.Players = slices.Delete(e.state.Players, playerIndex, playerIndex+1)

	e.turnManager.RemovePlayer(playerIndex)
	e.state.CurrentTurn = e.turnManager.Current()

	if len(e.state.Players) == 1 {
		e.state.Phase = Finished
		e.state.Winner = e.state.Players[0]
		e.broadcaster.Broadcast(Event{
			Type:     EventGameEnded,
			PlayerID: e.state.Winner.ID,
		})
	} else {
		e.broadcaster.Broadcast(Event{
			Sequence: int64(e.turnManager.Current()),
			Type:     EventTurnAdvanced,
		})
	}
}

// Close releases engine broadcaster resources. Safe to call multiple times.
func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.broadcaster != nil {
		e.broadcaster.Close()
	}
}
