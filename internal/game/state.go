package game

import (
	"slices"

	"github.com/Pieczasz/terminal-card/internal/deck"
)

// State is guarded by the owning Engine's mutex: every Rules method and every
// WithState callback runs with it held, which is why neither may call back into
// the Engine.
type State struct {
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
	return &State{
		// Cloned so removePlayerLocked's slices.Delete cannot reorder a slice the
		// caller still holds.
		Players: slices.Clone(players),
		Winner:  nil,
		Phase:   Waiting,
		Deck:    deck.New(cards),
		Rules:   rules,
	}
}
