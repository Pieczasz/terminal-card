package game

import "github.com/Pieczasz/terminal-card/internal/deck"

type Action interface {
	Name() string
}

type Event struct {
	Type     EventType
	PlayerID string
	Action   Action
}

type EventType uint8

const (
	EventUnknown EventType = iota
	EventTurnAdvanced
	EventActionApplied
	EventGameEnded
	EventGameStarted
	EventTurnTimedOut
	EventPlayerIdle
)

type PlayerSnapshot struct {
	ID       string
	Username string
	HandSize int
}

type StateSnapshot struct {
	Phase         Phase
	CurrentPlayer string
	TopDiscard    deck.Card
	DeckSize      int
	Players       []PlayerSnapshot
	Winner        string
}
