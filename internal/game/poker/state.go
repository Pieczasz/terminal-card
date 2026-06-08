package poker

import (
	"terminalcard/internal/deck"
	"terminalcard/internal/game"
	"terminalcard/internal/player"
)

type State struct {
	MainPool     uint
	CurrentBet   uint16
	LastAction   game.ActionType
	PlayerRaised *player.Player
	PlayersFold  []*player.Player
	Table        []*deck.Card
}
