package game

import (
	"time"

	"github.com/Pieczasz/terminal-card/internal/deck"
)

type Rules interface {
	MinPlayers() int
	MaxPlayers() int

	InitialDeck() []deck.Card
	InitialDealCount() int

	OnGameStart(state *State) error

	ValidateAction(state *State, action Action) error
	AfterAction(state *State, action Action) error
	ApplyAction(state *State, action Action)
	CheckWinCondition(state *State) bool
	Standings(state *State) []*Player
}

type TurnTimeoutHandler interface {
	TimeoutAction(state *State) Action
}
type TurnDurationHandler interface {
	TurnTimeout(state *State) time.Duration
}

type PlayerLeaveHandler interface {
	OnPlayerLeave(state *State, playerID string)
	AfterPlayerRemoved(state *State, removedIndex int)
}

// StandingScorer reports the value Standings ordered a player by. Equal scores are
// a genuine draw, and Engine.StandingsWithPlaces turns them into equal finishing
// places so the rating calculation can settle them as one - without it a tie is
// separated by slice position and the seat that happened to sort first takes rating
// off the seat that did not.
//
// Only equality is read, so the sign and direction do not matter: return whatever
// the sort compares. A rules set whose ordering is already total can skip this.
type StandingScorer interface {
	StandingScore(state *State, p *Player) int
}

func AnyScoreAtLeast(scores map[string]int, target int) bool {
	for _, score := range scores {
		if score >= target {
			return true
		}
	}
	return false
}
