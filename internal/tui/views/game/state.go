package game

import (
	"fmt"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
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
}

// SyncBaseState builds a redacted view via BoundEngine (own hand only).
func SyncBaseState(global router.GlobalContext, bound *game.BoundEngine) BaseState {
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

	if global.User != nil && base.Phase == game.Playing {
		base.MyTurn = bound.Engine() != nil &&
			bound.Engine().CurrentPlayerID() == fmt.Sprint(global.User.ID)
	}

	viewerName := ""
	if global.User != nil {
		viewerName = global.User.Username
	}
	for _, opp := range snap.Players {
		if opp.ID == viewerName {
			continue
		}
		base.Opponents = append(base.Opponents, opp)
	}

	return base
}
