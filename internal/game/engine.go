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
	"time"

	"github.com/Pieczasz/terminal-card/internal/broadcaster"
	"github.com/Pieczasz/terminal-card/internal/deck"
)

type Engine struct {
	mu          sync.Mutex
	state       *State
	broadcaster *broadcaster.Broadcaster[Event]

	turnTimeout  time.Duration
	turnSeq      uint64
	turnTimer    *time.Timer
	turnDeadline time.Time
	missedTurns  map[string]int
}

type EngineOption func(*Engine)

func WithTurnTimeout(d time.Duration) EngineOption {
	return func(e *Engine) {
		e.turnTimeout = d
	}
}

func NewEngine(rules Rules, players []*Player, cards []deck.Card, opts ...EngineOption) *Engine {
	e := &Engine{
		state: NewState(rules, players, cards),
		// Headroom above the player count for non-player subscribers (the lobby's
		// ranked-finalize watcher) and brief reconnect overlap; too small a cap
		// hands a closed channel to a real player, freezing their view.
		// TODO: when we add the possibility to view other's games
		// this needs to be handled differently.
		broadcaster: broadcaster.New[Event](len(players) + 8),
		turnTimeout: DefaultTurnTimeout,
		missedTurns: make(map[string]int, len(players)),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

func (e *Engine) Broadcaster() *broadcaster.Broadcaster[Event] {
	return e.broadcaster
}

func (e *Engine) WithState(fn func(state *State)) {
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
	fn(e.state)
}

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
			snap.Winner = state.Winner.DisplayName()
		}
		snap.Players = make([]PlayerSnapshot, 0, len(state.Players))
		if current := e.currentPlayerLocked(); current != nil {
			snap.CurrentPlayer = current.DisplayName()
			snap.CurrentPlayerID = current.ID
		}
		for _, p := range state.Players {
			if p == nil {
				continue
			}
			snap.Players = append(snap.Players, PlayerSnapshot{
				ID:       p.ID,
				Username: p.DisplayName(),
				HandSize: len(p.Cards),
			})
		}
	})
	return snap
}

func (e *Engine) CurrentPlayerID() string {
	e.mu.Lock()
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
	defer e.mu.Unlock()
	current := e.currentPlayerLocked()
	if current == nil {
		return ""
	}
	return current.ID
}

func (e *Engine) IsFinished() bool {
	e.mu.Lock()
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
	defer e.mu.Unlock()
	return e.state.Phase == Finished
}

func (e *Engine) Standings() []*Player {
	e.mu.Lock()
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
	defer e.mu.Unlock()
	return e.standingsLocked()
}

// StandingsWithPlaces returns the standings and their 1-based finishing places under
// a single lock hold, so the two cannot describe different moments. Players the rules
// scored equally share a place; everything else counts up strictly.
func (e *Engine) StandingsWithPlaces() ([]*Player, []int) {
	e.mu.Lock()
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
	defer e.mu.Unlock()

	standings := e.standingsLocked()
	return standings, e.placesLocked(standings)
}

func (e *Engine) placesLocked(standings []*Player) []int {
	places := make([]int, len(standings))
	scorer, ok := e.state.Rules.(StandingScorer)
	for i, p := range standings {
		switch {
		case i == 0:
			places[i] = 1
		case ok && p != nil && standings[i-1] != nil &&
			scorer.StandingScore(e.state, p) == scorer.StandingScore(e.state, standings[i-1]):
			places[i] = places[i-1]
		default:
			places[i] = i + 1
		}
	}
	return places
}

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

func (e *Engine) standingsLocked() []*Player {
	standings := e.state.Rules.Standings(e.state)

	placed := make(map[string]bool, len(standings))
	for _, p := range standings {
		if p != nil {
			placed[p.ID] = true
		}
	}

	out := make([]*Player, 0, len(standings)+len(e.state.LeftPlayers))
	out = append(out, standings...)
	for _, p := range slices.Backward(e.state.LeftPlayers) {
		if p != nil && !placed[p.ID] {
			out = append(out, p)
		}
	}
	return out
}

// Start deals and opens the table. Everything that can fail runs before the phase
// moves off Waiting, so a failed start leaves a lobby that can try again rather
// than a table stuck mid-deal.
func (e *Engine) Start() error {
	e.mu.Lock()
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
	defer e.mu.Unlock()

	if e.state.Phase != Waiting {
		return errors.New("game already started")
	}
	if len(e.state.Players) == 0 {
		return errors.New("cannot start game with no players")
	}

	if err := e.state.Deck.Shuffle(); err != nil {
		return fmt.Errorf("shuffle deck: %w", err)
	}

	hands := make([][]deck.Card, len(e.state.Players))
	for playerIdx := range e.state.Players {
		cards, ok := e.state.Deck.DrawNCards(e.state.Rules.InitialDealCount())
		if !ok {
			return errors.New("insufficient number of cards to deal for all players")
		}
		hands[playerIdx] = cards
	}

	startIdx, err := cryptoIntN(len(e.state.Players))
	if err != nil {
		return fmt.Errorf("selecting first player: %w", err)
	}

	for playerIdx, hand := range hands {
		e.state.Players[playerIdx].Cards = hand
	}
	e.state.Phase = Playing
	e.state.CurrentTurn = startIdx

	if err := e.state.Rules.OnGameStart(e.state); err != nil {
		e.state.Phase = Waiting
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
	if currentPlayer == nil || currentPlayer.ID != playerID {
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
func (e *Engine) finishGameLocked(fallback *Player) {
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

func (e *Engine) RemovePlayer(playerID string) {
	e.mu.Lock()
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
	defer e.mu.Unlock()
	e.removePlayerLocked(playerID)
}

// removePlayerLocked is the body of RemovePlayer. Caller must hold e.mu and e.state.mu.
func (e *Engine) removePlayerLocked(playerID string) {
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

	if e.state.CurrentTurn > playerIndex {
		e.state.CurrentTurn--
	}
	e.clampTurnLocked()

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

// currentPlayerLocked is the seat State.CurrentTurn points at, or nil when it points
// at no seat. Caller must hold e.state.mu.
func (e *Engine) currentPlayerLocked() *Player {
	if e.state.CurrentTurn < 0 || e.state.CurrentTurn >= len(e.state.Players) {
		return nil
	}
	return e.state.Players[e.state.CurrentTurn]
}

func (e *Engine) applyNextTurnLocked(advance bool) {
	switch {
	case e.state.OverrideNextTurn != nil:
		e.state.CurrentTurn = *e.state.OverrideNextTurn
		e.state.OverrideNextTurn = nil
	case advance:
		e.state.CurrentTurn++
	}
	e.clampTurnLocked()
	e.armTurnTimerLocked()
}

// clampTurnLocked forces State.CurrentTurn into [0, len(Players)) so a stale index
// from a leave handler or a rules override cannot name a seat that is gone.
func (e *Engine) clampTurnLocked() {
	n := len(e.state.Players)
	if n <= 0 {
		e.state.CurrentTurn = 0
		return
	}
	e.state.CurrentTurn = ((e.state.CurrentTurn % n) + n) % n
}
