package game

import (
	"client/internal/deck"
	"client/internal/player"
)

type Rules interface {
	Name() string
	MinPlayers() int
	MaxPlayers() int

	InitialDeck() []deck.Card
	InitialDealCount() int

	PreActionCondition(state *State, action Action) error
	PostActionCondition(state *State, action Action) error
	ApplyAction(state *State, action Action)
	CheckWinCondition(state *State) bool
}
