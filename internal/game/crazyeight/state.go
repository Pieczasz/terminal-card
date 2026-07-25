package crazyeight

import "github.com/Pieczasz/terminal-card/internal/deck"

// State holds Crazy Eights–specific game state stored in game.State.Extra.
type State struct {
	CurrentSuit deck.Suit
	// Passes counts consecutive turns where a player could not draw because both
	// the stock and discard are exhausted. When it reaches the player count the
	// hand is deadlocked and ends, scored by fewest cards held.
	Passes int
}
