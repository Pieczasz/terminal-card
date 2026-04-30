// Package game contains game logic handling and initialization of new game state,
// handling different rules, player seats, connections, state and turns.
package game

import (
	"client/internal/broadcaster"
	"client/internal/deck"
	"client/internal/player"
)

type Game struct {
	players     []*player.Player
	winner      *player.Player
	broadcaster *broadcaster.Broadcaster
	state       state
	deck        *deck.Pile
}

type state uint

const (
	InProgress state = iota
	Finished
)

func New(players []*player.Player, cards []deck.Card) *Game {
	game := &Game{
		players:     players,
		winner:      nil,
		broadcaster: broadcaster.New(len(players)),
		state:       InProgress,
		deck:        deck.New(cards),
	}

	return game
}
