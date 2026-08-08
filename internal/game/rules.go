package game

import (
	"time"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/player"
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
	Standings(state *State) []*player.Player
}

// TurnTimeoutHandler is an optional Rules extension that supplies the move to play
// for a player who let their turn clock run out. Rules that do not implement it get
// no turn clock at all, so a game only gains one once it can say what a safe move is.
//
// The returned action must be one ValidateAction accepts for the player currently on
// turn: the engine submits it through the ordinary path, so an illegal action is
// refused like any other. Returning nil means there is no safe move, and the engine
// takes the player's seat instead of guessing.
//
// Called with the engine and state locks held, under the same contract as Rules.
type TurnTimeoutHandler interface {
	TimeoutAction(state *State) Action
}

// TurnDurationHandler is an optional Rules extension that sets how long a particular
// turn is worth. Returning zero, or not implementing it at all, leaves the engine's
// configured timeout in place.
//
// It exists because not every turn asks the same thing of a player: a betting
// decision under pressure is not the same as being asked whether to deal the next
// hand, and holding both to one clock either rushes the first or stalls the table on
// the second. It cannot resurrect a clock that WithTurnTimeout disabled.
//
// Called with the engine and state locks held, under the same contract as Rules.
type TurnDurationHandler interface {
	TurnTimeout(state *State) time.Duration
}

// PlayerLeaveHandler is an optional Rules extension for mid-hand disconnects.
// OnPlayerLeave runs before the player is removed from state.Players.
// AfterPlayerRemoved runs after removal (seat indices already shifted).
type PlayerLeaveHandler interface {
	OnPlayerLeave(state *State, playerID string)
	AfterPlayerRemoved(state *State, removedIndex int)
}
