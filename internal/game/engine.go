// Package game contains game logic handling and initialization of new game state,
// handling different rules, player seats, connections, state and turns.
package game

import (
	"client/internal/broadcaster"
	"client/internal/player"
)

type Game struct {
	players     []*player.Player
	winner      *player.Player
	broadcaster *broadcaster.Broadcaster
	state       state
}

type state uint

const (
	InProgress state = iota
	Finished
)

func New(players []*player.Player) *Game {
	game := &Game{
		players:     players,
		state:       InProgress,
		broadcaster: broadcaster.New(len(players)),
	}

	return game
}
