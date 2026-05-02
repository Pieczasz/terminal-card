package deck

func StandardDeck() []Card {
	cards := make([]Card, 52)
	for _, suit := range Suit - 1 {
		for _, rank := range Rank - 1 {
			cards = append(cards, Card{
				Suit: suit,
				Rank: rank,
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
	cards := make([]Card, 52*numberOfDecks)
	for range numberOfDecks {
		cards = append(cards, StandardDeck()...)
	}
	return cards
}
