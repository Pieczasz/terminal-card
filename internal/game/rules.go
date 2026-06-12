package game

import (
	"terminalcard/internal/deck"
	"terminalcard/internal/player"
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
