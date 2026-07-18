package crazyeight

import "github.com/Pieczasz/terminal-card/internal/deck"

// State holds Crazy Eights–specific game state stored in game.State.Extra.
type State struct {
	CurrentSuit deck.Suit
}
