package game

import (
	"fmt"
	"terminalcard/internal/deck"
	"terminalcard/internal/game"
	"terminalcard/internal/tui/router"
)

// BaseState contains standard engine state applicable to most games.
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
			base.Winner = state.Winner.DatabaseUser.Username
			return
		}

		if base.Phase == game.Waiting {
			return
		}

		base.CurrentPlayer = state.Players[state.CurrentTurn].DatabaseUser.Username
		base.MyTurn = base.CurrentPlayer == global.User.Username

		for _, p := range state.Players {
			if fmt.Sprint(p.DatabaseUser.ID) == fmt.Sprint(global.User.ID) {
				base.Hand = p.Cards
			} else {
				base.Opponents = append(base.Opponents, game.PlayerSnapshot{
					ID:       p.DatabaseUser.Username,
					HandSize: len(p.Cards),
				})
			}
		}

		top, _ := state.Discard.Peak()
		base.TopDiscard = top
	})

	return base
}
