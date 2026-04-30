package deck

type Card struct {
	Rank Rank
	Suit Suit
}

type Rank uint8

const (
	Ace Rank = iota
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

type Suit uint8

const (
	Spades Suit = iota
	Hearts
	Diamonds
	Clubs
	NoSuit
)
