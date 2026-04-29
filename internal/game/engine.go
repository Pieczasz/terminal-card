// Package game contains game logic handling and initialization of new game state,
// handling different rules, player seats, connections, state and turns.
package game

import "client/internal/player"

type Game struct {
	players []*player.Player
	broadcaster
	state state
}

type state uint

const (
	InProgress state = iota
	Finished
)

func NewGame(players []*player.Player) *Game {
	game := &Game{
		players: players,
		state:   InProgress,
	}

	return game
}
