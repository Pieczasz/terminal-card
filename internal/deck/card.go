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
	// Uno ranks (additive; StandardDeck never deals them).
	Zero
	One
	Skip
	Reverse
	DrawTwo
	Wild
	WildDrawFour
)

type Suit uint8

const (
	Spades Suit = iota
	Hearts
	Diamonds
	Clubs
	NoSuit
)
