package game

import (
	"slices"
	"time"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
)

// BaseState contains a standard engine state applicable to most games.
type BaseState struct {
	Phase      game.Phase
	MyTurn     bool
	Hand       []deck.Card
	TopDiscard deck.Card
	// Seats is every player in engine seat order, hero included; Opponents is the
	// same list with the hero dropped. Games that lay players out by seat (Hearts,
	// Gin Rummy) need the former and would otherwise reach past BoundEngine for it.
	Seats     []game.PlayerSnapshot
	Opponents []game.PlayerSnapshot
	DeckSize  int
	// CurrentPlayer is for rendering only. Compare CurrentPlayerID to decide whose
	// turn a seat is showing: display names are not unique.
	CurrentPlayer   string
	CurrentPlayerID string
	Winner          string
	// TurnRemaining is how long the player on turn has left before the engine plays
	// for them. Zero means no clock is running.
	TurnRemaining time.Duration
}

// SyncBaseState builds a redacted view via BoundEngine (own hand only), and hands the
// per-game state to extra in the same lock hold when one is given.
//
// Every identity comparison here goes through bound.PlayerID, which is the
// authenticated session's player: display names are for rendering, never for
// deciding whose hand or whose turn this is.
func SyncBaseState(bound *game.BoundEngine, extra func(any)) BaseState {
	var base BaseState
	if bound == nil {
		return base
	}

	snap, hand, remaining := bound.Frame(extra)
	base.Phase = snap.Phase
	base.TopDiscard = snap.TopDiscard
	base.CurrentPlayer = snap.CurrentPlayer
	base.CurrentPlayerID = snap.CurrentPlayerID
	base.Winner = snap.Winner
	base.Hand = hand
	base.TurnRemaining = remaining
	base.Seats = snap.Players
	base.DeckSize = snap.DeckSize

	// Everything above came out of one Frame, which is one hold of the engine lock.
	// The pieces cannot describe different moments: MyTurn is decided off the same
	// snapshot as CurrentPlayerID, and the hand off the same one again. Every seat
	// renderer highlights from CurrentPlayerID while the hand, hints and clock light
	// up from MyTurn - so reads that straddled a turn change used to put the
	// highlight on one seat and the controls on another, and tell the player to wait
	// for a hand they could actually play.
	heroID := bound.PlayerID()
	base.MyTurn = base.Phase == game.Playing && heroID != "" && snap.CurrentPlayerID == heroID

	base.Opponents = slices.DeleteFunc(slices.Clone(snap.Players), func(p game.PlayerSnapshot) bool {
		return p.ID == heroID
	})

	return base
}

// SeatNames maps player ID to display name for every seat. PlayerSnapshot.Username
// already falls back to the player ID when there is no database user behind the seat.
func (b BaseState) SeatNames() map[string]string {
	names := make(map[string]string, len(b.Seats))
	for _, seat := range b.Seats {
		names[seat.ID] = seat.Username
	}
	return names
}

// SeatOrder is the player IDs in engine seat order.
func (b BaseState) SeatOrder() []string {
	order := make([]string, len(b.Seats))
	for i, seat := range b.Seats {
		order[i] = seat.ID
	}
	return order
}
