package game

import "github.com/Pieczasz/terminal-card/internal/deck"

type Action interface {
	Name() string
}

type Event struct {
	Type     EventType
	PlayerID string
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
	Phase Phase
	// CurrentPlayer is a display name; CurrentPlayerID is what identifies the seat.
	// Two players can share a name, so never decide whose turn it is from the former.
	CurrentPlayer   string
	CurrentPlayerID string
	TopDiscard      deck.Card
	DeckSize        int
	Players         []PlayerSnapshot
	Winner          string
}
