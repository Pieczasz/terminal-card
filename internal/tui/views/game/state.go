package game

import (
	"slices"
	"time"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
)

// BaseState is the engine state every game view renders from.
type BaseState struct {
	Phase      game.Phase
	MyTurn     bool
	Hand       []deck.Card
	TopDiscard deck.Card
	// Seats is every player in engine seat order, hero included; Opponents drops the hero.
	Seats     []game.PlayerSnapshot
	Opponents []game.PlayerSnapshot
	DeckSize  int
	// CurrentPlayer is for rendering only; decide turns on CurrentPlayerID, since
	// display names are not unique.
	CurrentPlayer   string
	CurrentPlayerID string
	Winner          string
	// TurnRemaining is time left before the engine plays for them; zero means no clock.
	TurnRemaining time.Duration
}

// SyncBaseState builds a redacted view via BoundEngine (own hand only), handing the
// per-game state to extra in the same lock hold. Identity comes from bound.PlayerID,
// the authenticated session's player, never from a display name.
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

	// MyTurn comes off the same Frame as CurrentPlayerID: seats highlight from the
	// latter while the hand and clock light up from the former, so reads that straddled
	// a turn change put the highlight on one seat and the controls on another.
	heroID := bound.PlayerID()
	base.MyTurn = base.Phase == game.Playing && heroID != "" && snap.CurrentPlayerID == heroID

	base.Opponents = slices.DeleteFunc(slices.Clone(snap.Players), func(p game.PlayerSnapshot) bool {
		return p.ID == heroID
	})

	return base
}

// SeatNames maps player ID to display name; Username falls back to the ID itself.
func (b BaseState) SeatNames() map[string]string {
	names := make(map[string]string, len(b.Seats))
	for _, seat := range b.Seats {
		names[seat.ID] = seat.Username
	}
	return names
}

func (b BaseState) SeatOrder() []string {
	order := make([]string, len(b.Seats))
	for i, seat := range b.Seats {
		order[i] = seat.ID
	}
	return order
}
