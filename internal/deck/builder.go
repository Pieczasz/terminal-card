package deck

func StandardDeck() []Card {
	cards := make([]Card, 0, 52)
	for s := Spades; s <= Clubs; s++ {
		for r := Ace; r <= King; r++ {
			cards = append(cards, Card{
				Suit: s,
				Rank: r,
			})
		}
	}
	return cards
}
