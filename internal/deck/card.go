package deck

import "slices"

type Card struct {
	Rank Rank
	Suit Suit
}

type Suit uint8

// NoSuit is the zero Suit so an unset one is detectably empty, the same reason
// standard ranks start at 1. Wilds and jokers carry it deliberately.
const (
	NoSuit Suit = iota
	Spades
	Hearts
	Diamonds
	Clubs
)

type Rank uint8

const (
	Ace Rank = iota + 1
	Two
	Three
	Four
	Five
	Six
	Seven
	Eight
	Nine
	Ten
	Jack
	Queen
	King
	Joker
)

// TODO: isn't this approach shit?
const (
	Zero Rank = iota + 20
	One
	Skip
	Reverse
	DrawTwo
	Wild
	WildDrawFour
)

// AllRanks is every defined Rank, in ascending order. It exists so exhaustiveness can
// be tested: a map keyed by Rank has no compiler check, unlike a switch.
var AllRanks = []Rank{
	Ace, Two, Three, Four, Five, Six, Seven, Eight, Nine, Ten, Jack, Queen, King, Joker,
	Zero, One, Skip, Reverse, DrawTwo, Wild, WildDrawFour,
}

// RankValue is a standard playing-card rank's comparison value, Ace high at 14.
func RankValue(r Rank) int {
	switch {
	case r == Ace:
		return 14
	case r >= Two && r <= Joker:
		return int(r)
	default:
		return 0
	}
}

func RunOrder(r Rank) int {
	if r < Ace || r > King {
		return 0
	}
	return int(r)
}

func PipValue(r Rank) int {
	return min(RunOrder(r), 10)
}

func RemoveOne(hand []Card, card Card) []Card {
	i := slices.Index(hand, card)
	if i < 0 {
		return slices.Clone(hand)
	}
	return slices.Delete(slices.Clone(hand), i, i+1)
}

func RemoveEach(hand []Card, cards []Card) []Card {
	out := slices.Clone(hand)
	for _, c := range cards {
		if i := slices.Index(out, c); i >= 0 {
			out = slices.Delete(out, i, i+1)
		}
	}
	return out
}
