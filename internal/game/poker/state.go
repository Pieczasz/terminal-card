package poker

import (
	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/player"
)

type State struct {
	DealerIndex  int
	MainPool     uint
	CurrentBet   uint
	SmallBlind   uint
	BigBlind     uint
	Phase        RoundPhase
	LastAction   game.Action
	PlayerRaised *player.Player
	PlayersFold  []*player.Player
	Table        []*deck.Card
	PlayerChips  map[string]uint
	PlayerBets   map[string]uint
}

type RoundPhase uint8

const (
	PreFlop RoundPhase = iota
	Flop
	Turn
	River
	Showdown
)
