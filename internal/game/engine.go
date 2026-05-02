// Package game contains game logic handling and initialization of new game state,
// handling different rules, player seats, connections, state and turns.
package game

import (
	"client/internal/broadcaster"
	"client/internal/deck"
	"client/internal/player"
	"errors"
	"fmt"
	"sync"
)

type Engine struct {
	mu          sync.Mutex
	state       *State
	turnManager *TurnManager
	broadcaster *broadcaster.Broadcaster
	registry    *Registry
}

func NewGameEngine(rules Rules, players []*player.Player, cards []deck.Card) *Engine {
	return &Engine{
		state:       NewState(players, cards),
		turnManager: NewTurnManager(len(players)),
		broadcaster: broadcaster.New(len(players)),
		registry:    NewRegistry(),
	}
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

	// TODO: refactor this sphagetti?
	if e.state.Players[e.turnManager.Current()].Id != playerId {
		return errors.New("wait for your turn to perform an action")
	}

	if err := e.state.Rules.PreActionCondition(e.state, action); err != nil {
		return fmt.Errorf("you can't perform that action %w", err)
	}

	e.state.Rules.ApplyAction(e.state, action)
	// TODO: broadcast

	if err := e.state.Rules.PostActionCondition(e.state, action); err != nil {
		// TODO: slog this and trace it, maybe revert the action
		return fmt.Errorf("post condition doesn't hold %w", err)
	}

	if e.state.Rules.CheckWinCondition(e.state) {
		// TODO: check if this won't interfere with defer calls
		finishGame(e.state.Players[e.turnManager.Current()].Id)
	}
	e.turnManager.Next()

	return nil
}
