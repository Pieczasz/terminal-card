package deck

type Card struct {
	Rank rank
	Suit suit
}

type rank uint

const (
	Ace rank = iota
	One
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

type suit uint

const (
	Spades suit = iota
	Hearts
	Diamonds
	Clubs
)
