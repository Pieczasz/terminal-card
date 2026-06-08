// Package game contains game logic handling and initialization of new game state,
// handling different rules, player seats, connections, state and turns.
package game

import (
	"sync"
	"terminalcard/internal/deck"
	"terminalcard/internal/player"
)

type State struct {
	mu sync.RWMutex

	Players     []*player.Player
	CurrentTurn int
	Phase       Phase
	Winner      *player.Player

	Deck    *deck.Pile
	Discard *deck.Pile
	Rules   Rules
	Extra   any
}

type Phase uint8

const (
	Waiting Phase = iota
	Dealing
	Playing
	Finished
)

func NewState(rules Rules, players []*player.Player, cards []deck.Card) *State {
	state := &State{
		Players: players,
		Winner:  nil,
		Phase:   Waiting,
		Deck:    deck.New(cards),
		Rules:   rules,
	}

	return state
}
