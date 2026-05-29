// Package game contains game logic handling and initialization of new game state,
// handling different rules, player seats, connections, state and turns.
package game

import (
	"client/internal/broadcaster"
	"client/internal/deck"
	"client/internal/player"
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

type Engine struct {
	mu          sync.Mutex
	state       *State
	turnManager *TurnManager
	broadcaster *broadcaster.Broadcaster[Event]
	registry    *Registry
}

func NewGameEngine(rules Rules, players []*player.Player, cards []deck.Card) *Engine {
	return &Engine{
		state:       NewState(players, cards),
		turnManager: NewTurnManager(len(players)),
		broadcaster: broadcaster.New[Event](len(players)),
		registry:    NewRegistry(),
	}
}

func (e *Engine) CurrentPlayer() *player.Player {
	return e.state.Players[e.turnManager.Current()]
}

func (e *Engine) Broadcaster() *broadcaster.Broadcaster[Event] {
	return e.broadcaster
}

func (e *Engine) Start() error {
	e.mu.Lock()
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
	defer e.mu.Unlock()

	if len(e.state.Players) > e.state.Rules.MaxPlayers() {
		return errors.New("number of players in lobby exceeds maximum number of players for this game")
	}
	if len(e.state.Players) < e.state.Rules.MinPlayers() {
		return errors.New("number of players in lobby is less then minimum number of players for this game")
	}

	e.state.Phase = Dealing

	deck := deck.New(e.state.Rules.InitialDeck())
	for playerIdx := range e.state.Players {
		cards, ok := deck.DrawNCards(e.state.Rules.InitialDealCount())
		if !ok {
			return errors.New("insufficient number of cards to deal for all players")
		}
		e.state.Players[playerIdx].Cards = cards
	}

	e.state.Phase = Playing

	return nil
}

func (e *Engine) SubmitAction(playerId string, action Action) error {
	e.mu.Lock()
	e.state.mu.Lock()
	defer e.state.mu.Unlock()
	defer e.mu.Unlock()

	if e.state.Phase != Playing {
		return errors.New("game not in playing phase")
	}

	currentPlayer := e.CurrentPlayer()
	if currentPlayer.Id != playerId {
		//TODO: cheating?
		return errors.New("wait for your turn to perform an action")
	}

	if err := e.state.Rules.PreActionCondition(e.state, action); err != nil {
		return fmt.Errorf("you can't perform that action %w", err)
	}

	e.state.Rules.ApplyAction(e.state, action)

	e.broadcaster.Broadcast(Event{
		Sequence: int64(e.turnManager.Current()), // Basic sequence for now
		Type:     EventCardPlayed,
		PlayerID: playerId,
		Action:   action,
		// State snapshot could be built here
	})

	if err := e.state.Rules.PostActionCondition(e.state, action); err != nil {
		//TODO:
		// this can mean cheating, we should detect that and prevent it
		slog.Error("post condition doesn't hold for a game, cheating?", "error", err)
		return fmt.Errorf("post condition doesn't hold %w", err)
	}

	if e.state.Rules.CheckWinCondition(e.state) {
		e.state.Phase = Finished
		e.state.Winner = currentPlayer
		e.broadcaster.Broadcast(Event{
			Type:     EventGameEnded,
			PlayerID: currentPlayer.Id,
		})
		return nil
	}

	e.turnManager.Next()

	return nil
}
