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

// Uno's extra ranks sit in their own block well past King, which keeps three things
// true at once: the zero Rank is nobody's card, so a zero deck.Card is detectably
// empty rather than the ace of spades; the numbers 2..9 are shared with the standard
// ranks, so a card of one game is comparable with a card of the other; and adding a
// rank to either block cannot renumber the other.
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
//
// It is a package-level slice, so it is writable; nothing writes it, and nothing
// should. A function returning a fresh copy would make that unforgeable, at the cost
// of an allocation in every caller of what is a compile-time constant list.
var AllRanks = []Rank{
	Ace, Two, Three, Four, Five, Six, Seven, Eight, Nine, Ten, Jack, Queen, King, Joker,
	Zero, One, Skip, Reverse, DrawTwo, Wild, WildDrawFour,
}

// RankValue is a standard playing-card rank's comparison value, Ace high at 14.
// Anything outside Ace..King - the Uno block, the zero Rank, the Joker - has no
// comparison value and answers 0, the same as RunOrder and PipValue. No deck here
// deals jokers; ranking one alongside the standard cards would only decide a trick
// by accident, and 0 loses loudly instead of quietly tying the ace.
func RankValue(r Rank) int {
	switch {
	case r == Ace:
		return 14
	case r >= Two && r <= King:
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

// IsSuit reports whether s names one of the four real suits. A card that lets the
// player choose - an Eight, a Wild - is only playable once a suit is named, and NoSuit,
// a zero value and a garbage value from a client all have to be refused.
func IsSuit(s Suit) bool {
	switch s {
	case Spades, Hearts, Diamonds, Clubs:
		return true
	default:
		return false
	}
}
