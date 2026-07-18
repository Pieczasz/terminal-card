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

func StandardDeckWithNJokers(numberOfJokers int) []Card {
	cards := StandardDeck()
	for range numberOfJokers {
		cards = append(cards, Card{
			Suit: NoSuit,
			Rank: Joker,
		})
	}
	return cards
}

func MultipleStandardDecks(numberOfDecks int) []Card {
	cards := make([]Card, 0, 52*numberOfDecks)
	for range numberOfDecks {
		cards = append(cards, StandardDeck()...)
	}
	return cards
}
