package poker

import (
	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/player"
)

// Pot is a main or side pot with players eligible to win it.
type Pot struct {
	Amount   uint
	Eligible []string // player IDs
}

type State struct {
	DealerIndex int
	SBIndex     int
	BBIndex     int

	MainPool   uint
	CurrentBet uint
	MinRaise   uint
	SmallBlind uint
	BigBlind   uint

	Phase      RoundPhase
	LastAction game.Action

	Folded       map[string]bool
	PlayersAllIn map[string]bool

	Table []deck.Card

	PlayerChips      map[string]uint
	PlayerBets       map[string]uint // chips committed this street
	TotalContributed map[string]uint // chips committed this hand (for side pots)
	ActedThisRound   map[string]bool

	Pots         []Pot
	HandComplete bool
	Winners      []*player.Player
}

type RoundPhase uint8

const (
	PhaseUnknown RoundPhase = iota
	PreFlop
	Flop
	Turn
	River
	Showdown
)

func (p RoundPhase) String() string {
	switch p {
	case PreFlop:
		return "PREFLOP"
	case Flop:
		return "FLOP"
	case Turn:
		return "TURN"
	case River:
		return "RIVER"
	case Showdown:
		return "SHOWDOWN"
	default:
		return "UNKNOWN"
	}
}
