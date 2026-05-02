// Package game contains game logic handling and initialization of new game state,
// handling different rules, player seats, connections, state and turns.
package game

import (
	"client/internal/broadcaster"
	"client/internal/deck"
	"client/internal/player"
	"sync"
)

type State struct {
	mu sync.RWMutex

	Players     []*player.Player
	CurrentTurn int
	Phase       Phase
	Winner      *player.Player

	Broadcaster *broadcaster.Broadcaster

	Deck    *deck.Pile
	Discard *deck.Pile
	Rules   Rules
}

type Phase uint8

const (
	Waiting Phase = iota
	Dealing
	Playing
	Finished
)

func NewState(players []*player.Player, cards []deck.Card) *State {
	state := &State{
		Players:     players,
		Winner:      nil,
		Broadcaster: broadcaster.New(len(players)),
		Phase:       Waiting,
		Deck:        deck.New(cards),
	}

	return state
}

func (e *Engine) Start() error {
	e.state.mu.Lock()
	defer e.state.mu.Unlock()

	return nil

}
