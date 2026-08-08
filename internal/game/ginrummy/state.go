package ginrummy

import (
	"time"

	"github.com/Pieczasz/terminal-card/internal/deck"
)

type Phase uint8

const (
	AwaitingDraw Phase = iota
	AwaitingDiscard
	HandOver
)

const (
	KnockThreshold = 10
	GinBonus       = 25
	UndercutBonus  = 25
	TargetScore    = 100
	WallStockSize  = 2
	dealCount      = 10

	HandOverTimeout = time.Minute
)

// State is Gin Rummy match state stored in game.State.Extra.
type State struct {
	HandPhase Phase

	// FirstActor is the seat (0 or 1) that acted first in the current hand.
	// Alternates every hand. Used to park the cursor between hands.
	FirstActor int

	HandNumber int

	// CumulativeScores: higher is better. Standings sorts descending.
	CumulativeScores map[string]int

	HandComplete  bool
	MatchComplete bool

	// LastHandResult: settle-up summary for the hand that just ended.
	LastHandResult *HandResult
}

// HandResult is the settle-up summary shown between hands.
type HandResult struct {
	Knocker string // player ID; empty on Wall

	KnockerMelds          [][]deck.Card
	KnockerDeadwood       []deck.Card
	KnockerDeadwoodPoints int

	OpponentMelds          [][]deck.Card
	OpponentDeadwood       []deck.Card
	OpponentDeadwoodPoints int
	LaidOffCards           []deck.Card

	Gin        bool
	Undercut   bool
	Wall       bool
	ScoreDelta int
	Winner     string // player ID credited; empty on Wall
}
