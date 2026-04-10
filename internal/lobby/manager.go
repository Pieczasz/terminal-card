// Package lobby contains implementation of game lobby - it is responsible for
// random game code generations, players game invitations, starting the game,
// selecting card game and options.
package lobby

import (
	"math/rand"
)

// Lobby contains all information related to a lobby.
type Lobby struct {
	leader   *Player
	guests   []*Player
	options  *lobbyOptions
	gameCode string
}

type lobbyOptions struct {
	cardGame   Game
	maxPlayers int
	isPrivate  bool
}

// NewLobby returns a new Lobby with the leader, default options, and a random code.
func NewLobby(leader *Player, game Game) *Lobby {
	defaultOptions := &lobbyOptions{
		cardGame:   game,
		maxPlayers: 4,
		isPrivate:  false,
	}

	return &Lobby{
		leader:   leader,
		guests:   make([]*Player, 3),
		options:  defaultOptions,
		gameCode: generateGameCode(6),
	}
}

const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// generateGameCode creates a random string of the specified length.
func generateGameCode(length int) string {
	code := make([]byte, length)
	for i := range code {
		code[i] = charset[rand.Intn(len(charset))]
	}
	return string(code)
}
