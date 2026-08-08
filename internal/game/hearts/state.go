package hearts

import (
	"time"

	"github.com/Pieczasz/terminal-card/internal/deck"
)

type Stage uint8

const (
	StagePassing Stage = iota
	StageTrickPlay
	StageHandOver
)

type PassDirection uint8

const (
	PassLeft PassDirection = iota
	PassRight
	PassAcross
	PassNone
)

const (
	playerCount         = 4
	cardsPerHand        = 13
	cardsToPass         = 3
	penaltyPointsTotal  = 26
	DefaultTargetScore  = 100
	PassTurnTimeout     = 45 * time.Second
	HandOverTurnTimeout = time.Minute
)

var (
	twoOfClubs    = deck.Card{Rank: deck.Two, Suit: deck.Clubs}
	queenOfSpades = deck.Card{Rank: deck.Queen, Suit: deck.Spades}
)

// State is Hearts-specific match state stored in game.State.Extra.
type State struct {
	Stage Stage

	PassDirection PassDirection
	PendingPasses map[string][]deck.Card
	Passed        map[string]bool

	LedSuit      deck.Suit
	TrickCards   map[string]deck.Card
	TrickLeader  int
	HeartsBroken bool
	TricksPlayed int

	HandPoints       map[string]int
	CumulativeScores map[string]int
	HandNumber       int
	DealerIndex      int
	TargetScore      int
	HandComplete     bool
	MatchComplete    bool

	LastTrickWinner string
}

func resetHandState(extra *State) {
	extra.Stage = StagePassing
	extra.PassDirection = PassLeft
	extra.PendingPasses = nil
	extra.Passed = nil
	extra.LedSuit = deck.NoSuit
	extra.TrickCards = make(map[string]deck.Card, playerCount)
	extra.TrickLeader = 0
	extra.HeartsBroken = false
	extra.TricksPlayed = 0
	extra.HandPoints = make(map[string]int, playerCount)
	extra.HandComplete = false
	extra.LastTrickWinner = ""
}
