package crazyeight

import "client/internal/deck"

type State struct {
	Direction   int
	SkipNext    bool
	DrawStack   int
	CurrentSuit deck.Suit
}
