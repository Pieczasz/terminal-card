package game

import (
	"cmp"
	"slices"
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
	// ApplyAction mutates state for an action ValidateAction accepted. An error means
	// the state may be half-applied and the engine finishes the game, so anything
	// checkable up front belongs in ValidateAction.
	ApplyAction(state *State, action Action) error
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

// StandingScorer reports the value Standings ordered a player by, so
// Engine.StandingsWithPlaces can turn a tie into equal finishing places. Without it a
// draw is split by slice position and the seat that sorted first takes rating off the
// other.
//
// Only equality is read, so sign and direction do not matter. A rules set whose
// ordering is already total can skip this.
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

// StandingsByScore orders players by score ascending, ties stable by seat order, so a
// rules set's Standings and StandingScore cannot disagree. Negate the score to sort
// descending.
func StandingsByScore(players []*Player, score func(*Player) int) []*Player {
	standings := slices.Clone(players)
	slices.SortStableFunc(standings, func(a, b *Player) int {
		return cmp.Compare(score(a), score(b))
	})
	return standings
}
