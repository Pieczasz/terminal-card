package poker

import (
	"client/internal/deck"
	"client/internal/game"
	"client/internal/player"
)

type State struct {
	MainPool     uint
	CurrentBet   uint16
	LastAction   game.ActionType
	PlayerRaised *player.Player
	PlayersFold  []*player.Player
	Table        []*deck.Card
}
