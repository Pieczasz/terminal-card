package game

import (
	"time"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
)

// BaseState contains a standard engine state applicable to most games.
type BaseState struct {
	Phase         game.Phase
	MyTurn        bool
	Hand          []deck.Card
	TopDiscard    deck.Card
	Opponents     []game.PlayerSnapshot
	CurrentPlayer string
	Winner        string
	// TurnRemaining is how long the player on turn has left before the engine plays
	// for them. Zero means no clock is running.
	TurnRemaining time.Duration
}

// SyncBaseState builds a redacted view via BoundEngine (own hand only).
//
// Every identity comparison here goes through bound.PlayerID, which is the
// authenticated session's player: display names are for rendering, never for
// deciding whose hand or whose turn this is.
func SyncBaseState(bound *game.BoundEngine) BaseState {
	var base BaseState
	if bound == nil {
		return base
	}

	snap := bound.Snapshot()
	base.Phase = snap.Phase
	base.TopDiscard = snap.TopDiscard
	base.CurrentPlayer = snap.CurrentPlayer
	base.Winner = snap.Winner
	base.Hand = bound.Hand()
	base.TurnRemaining = bound.TurnRemaining()

	heroID := bound.PlayerID()
	if base.Phase == game.Playing && bound.Engine() != nil {
		base.MyTurn = bound.Engine().CurrentPlayerID() == heroID
	}

	for _, opp := range snap.Players {
		if opp.ID == heroID {
			continue
		}
		base.Opponents = append(base.Opponents, opp)
	}

	return base
}
