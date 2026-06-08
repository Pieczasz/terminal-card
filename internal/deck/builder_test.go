package deck

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuilder_StandardDeck(t *testing.T) {
	cards := StandardDeck()

	assert.Len(t, cards, 52, "standard deck should have exactly 52 cards")

	suitCounts := make(map[Suit]int)

	for _, card := range cards {
		assert.NotEqual(t, Joker, card.Rank, "standard deck should not contain jokers")
		assert.NotEqual(t, NoSuit, card.Suit, "standard deck should not contain cards without suit")
		suitCounts[card.Suit]++
	}

	assert.Equal(t, 13, suitCounts[Spades], "should have 13 spades")
	assert.Equal(t, 13, suitCounts[Hearts], "should have 13 hearts")
	assert.Equal(t, 13, suitCounts[Diamonds], "should have 13 diamonds")
	assert.Equal(t, 13, suitCounts[Clubs], "should have 13 clubs")
}
