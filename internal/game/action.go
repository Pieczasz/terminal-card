package game

import "terminalcard/internal/deck"

type Action interface {
	Name() string
}

type Event struct {
	Sequence int64
	Type     EventType
	PlayerID string
	Action   Action
	State    StateSnapshot
}

type EventType uint8

const (
	EventTurnAdvanced EventType = iota
	EventActionApplied
	EventGameEnded
	EventGameStarted
)

type PlayerSnapshot struct {
	ID       string
	HandSize int
}

type StateSnapshot struct {
	Phase         Phase
	CurrentPlayer string
	TopDiscard    deck.Card
	DeckSize      int
	HandSize      map[string]int
	Players       []PlayerSnapshot
	Winner        string
	Sequence      int64
}
