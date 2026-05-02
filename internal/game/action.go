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
	PlayCard ActionType = iota
	DrawCard
	PickSuit
	Pass
	Special
)

type Event struct {
	Sequence int64
	Type     EventType
	PlayerId string
	Action   Action
	State    StateSnapshot
}

type EventType uint8

const (
	PlayCard ActionType = iota
	DrawCard
	PickSuit
	Pass
	GameFinished
	Special
)

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
