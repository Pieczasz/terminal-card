package game

import (
	"fmt"
	"slices"

	"terminalcard/internal/deck"
	"terminalcard/internal/game"
	"terminalcard/internal/tui/router"
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

func SyncBaseState(global router.GlobalContext, engine *game.Engine) BaseState {
	var base BaseState
	if engine == nil {
		return base
	}

	engine.WithState(func(state *game.State) {
		base.Phase = state.Phase
		if base.Phase == game.Finished {
			if state.Winner != nil {
				base.Winner = state.Winner.Username()
			}
			return
		}

		if base.Phase == game.Waiting {
			return
		}

		if state.CurrentTurn < 0 || state.CurrentTurn >= len(state.Players) {
			return
		}

		current := state.Players[state.CurrentTurn]
		base.CurrentPlayer = current.Username()
		if global.User != nil {
			base.MyTurn = current.ID == fmt.Sprint(global.User.ID)
		}

		for _, p := range state.Players {
			if p == nil {
				continue
			}
			if global.User != nil && p.ID == fmt.Sprint(global.User.ID) {
				base.Hand = slices.Clone(p.Cards)
			} else {
				base.Opponents = append(base.Opponents, game.PlayerSnapshot{
					ID:       p.Username(),
					HandSize: len(p.Cards),
				})
			}
		}

		if state.Discard != nil {
			if top, ok := state.Discard.Peek(); ok {
				base.TopDiscard = top
			}
		}
	})

	return base
}
