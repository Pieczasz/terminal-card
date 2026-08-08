package uno

import "github.com/Pieczasz/terminal-card/internal/deck"

// State holds Uno-specific game state stored in game.State.Extra.
type State struct {
	CurrentColor deck.Suit // one of ColorRed/Yellow/Green/Blue once started
	Direction    int8      // +1 clockwise, -1 counterclockwise
	// Passes counts consecutive turns where a draw yielded nothing. When it
	// reaches the player count the hand is deadlocked and ends.
	Passes int
}

func isWild(r deck.Rank) bool {
	return r == Wild || r == WildDrawFour
}

func validColor(s deck.Suit) bool {
	switch s {
	case ColorRed, ColorYellow, ColorGreen, ColorBlue:
		return true
	default:
		return false
	}
}
