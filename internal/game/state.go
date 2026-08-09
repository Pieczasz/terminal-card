package game

import (
	"sync"

	"github.com/Pieczasz/terminal-card/internal/deck"
)

type State struct {
	mu sync.RWMutex

	Players          []*Player
	LeftPlayers      []*Player
	CurrentTurn      int
	OverrideNextTurn *int
	Phase            Phase
	Winner           *Player

	Deck    *deck.Pile
	Discard *deck.Pile
	Rules   Rules
	Extra   any
}

type Phase uint8

const (
	Waiting Phase = iota
	Playing
	Finished
)

func NewState(rules Rules, players []*Player, cards []deck.Card) *State {
	state := &State{
		Players: players,
		Winner:  nil,
		Phase:   Waiting,
		Deck:    deck.New(cards),
		Rules:   rules,
	}
	return state
}
