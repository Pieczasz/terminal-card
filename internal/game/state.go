package game

import (
	"sync"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/player"
)

type State struct {
	mu sync.RWMutex

	Players          []*player.Player
	LeftPlayers      []*player.Player
	CurrentTurn      int
	OverrideNextTurn *int
	Phase            Phase
	Winner           *player.Player

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
