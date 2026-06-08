package crazyeight

import "terminalcard/internal/deck"

type State struct {
	Direction   int
	SkipNext    bool
	DrawStack   int
	CurrentSuit deck.Suit
}
