package game

import "client/internal/deck"

type Action struct {
	Type   ActionType
	Cards  []deck.Card
	Suit   deck.Suit
	Target string
}

type ActionType uint8

const (
	ActionPlayCard ActionType = iota
	ActionDrawCard
	ActionPickSuit
	ActionPass
)

type Event struct {
	Sequence int64
	Type     EventType
	PlayerID string
	Action   Action
	State    StateSnapshot
}

type EventType uint8

const (
	EventCardPlayed EventType = iota
	EventCardDrawn
	EventSuitPicked
	EventTurnAdvanced
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
