package game

import (
	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/player"
)

type Rules interface {
	Name() string
	MinPlayers() int
	MaxPlayers() int

	InitialDeck() []deck.Card
	InitialDealCount() int

	OnGameStart(state *State) error

	PreActionCondition(state *State, action Action) error
	PostActionCondition(state *State, action Action) error
	ApplyAction(state *State, action Action)
	CheckWinCondition(state *State) bool
	GetStandings(state *State) []*player.Player
}

// PlayerLeaveHandler is an optional Rules extension for mid-hand disconnects.
// OnPlayerLeave runs before the player is removed from state.Players.
// AfterPlayerRemoved runs after removal (seat indices already shifted).
type PlayerLeaveHandler interface {
	OnPlayerLeave(state *State, playerID string)
	AfterPlayerRemoved(state *State, removedIndex int)
}
