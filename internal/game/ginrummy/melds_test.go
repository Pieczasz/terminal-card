package ginrummy

import (
	"testing"

	"github.com/Pieczasz/terminal-card/internal/deck"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func c(rank deck.Rank, suit deck.Suit) deck.Card {
	return deck.Card{Rank: rank, Suit: suit}
}

func TestBestMeldSplit_PureRun(t *testing.T) {
	t.Parallel()
	hand := []deck.Card{
		c(deck.Five, deck.Hearts), c(deck.Six, deck.Hearts), c(deck.Seven, deck.Hearts),
		c(deck.Eight, deck.Hearts),
	}
	melds, dw, pts := BestMeldSplit(hand)
	assert.Empty(t, dw)
	assert.Equal(t, 0, pts)
	require.Len(t, melds, 1)
	assert.True(t, isRun(melds[0]))
}

func TestBestMeldSplit_PureSet(t *testing.T) {
	t.Parallel()
	for _, size := range []int{3, 4} {
		t.Run(string(rune('0'+size)), func(t *testing.T) {
			t.Parallel()
			suits := []deck.Suit{deck.Spades, deck.Hearts, deck.Diamonds, deck.Clubs}
			hand := make([]deck.Card, size)
			for i := range size {
				hand[i] = c(deck.King, suits[i])
			}
			_, dw, pts := BestMeldSplit(hand)
			assert.Empty(t, dw)
			assert.Equal(t, 0, pts)
		})
	}
}

func TestBestMeldSplit_MixedWithDeadwood(t *testing.T) {
	t.Parallel()
	hand := []deck.Card{
		c(deck.Ace, deck.Spades), c(deck.Ace, deck.Hearts), c(deck.Ace, deck.Diamonds), // set
		c(deck.Four, deck.Clubs), c(deck.Five, deck.Clubs), c(deck.Six, deck.Clubs), // run
		c(deck.King, deck.Hearts), c(deck.Nine, deck.Spades), // deadwood 10+9
	}
	_, dw, pts := BestMeldSplit(hand)
	assert.Equal(t, 19, pts)
	assert.Len(t, dw, 2)
}

func TestBestMeldSplit_FullyDeadwood(t *testing.T) {
	t.Parallel()
	hand := []deck.Card{
		c(deck.Ace, deck.Spades), c(deck.Three, deck.Hearts), c(deck.Five, deck.Diamonds),
		c(deck.Seven, deck.Clubs), c(deck.Nine, deck.Spades),
	}
	melds, dw, pts := BestMeldSplit(hand)
	assert.Empty(t, melds)
	assert.Len(t, dw, 5)
	assert.Equal(t, 1+3+5+7+9, pts)
}

func TestBestMeldSplit_ExactGin(t *testing.T) {
	t.Parallel()
	hand := []deck.Card{
		c(deck.Two, deck.Hearts), c(deck.Three, deck.Hearts), c(deck.Four, deck.Hearts), c(deck.Five, deck.Hearts),
		c(deck.Jack, deck.Spades), c(deck.Jack, deck.Hearts), c(deck.Jack, deck.Diamonds),
		c(deck.Ace, deck.Clubs), c(deck.Ace, deck.Spades), c(deck.Ace, deck.Hearts),
	}
	_, dw, pts := BestMeldSplit(hand)
	assert.Empty(t, dw)
	assert.Equal(t, 0, pts)
}

func TestBestMeldSplit_AmbiguousFourAces(t *testing.T) {
	t.Parallel()
	// Four aces + 2♠ + 3♠: set of 3 aces + A♠-2♠-3♠ run covers everything.
	hand := []deck.Card{
		c(deck.Ace, deck.Spades), c(deck.Ace, deck.Hearts), c(deck.Ace, deck.Diamonds), c(deck.Ace, deck.Clubs),
		c(deck.Two, deck.Spades), c(deck.Three, deck.Spades),
	}
	_, dw, pts := BestMeldSplit(hand)
	assert.Empty(t, dw)
	assert.Equal(t, 0, pts)
}

func TestBestMeldSplit_AceLowRun_NoWraparound(t *testing.T) {
	t.Parallel()
	t.Run("A-2-3 valid", func(t *testing.T) {
		t.Parallel()
		hand := []deck.Card{
			c(deck.Ace, deck.Clubs), c(deck.Two, deck.Clubs), c(deck.Three, deck.Clubs),
		}
		_, dw, pts := BestMeldSplit(hand)
		assert.Empty(t, dw)
		assert.Equal(t, 0, pts)
	})
	t.Run("Q-K-A invalid", func(t *testing.T) {
		t.Parallel()
		hand := []deck.Card{
			c(deck.Queen, deck.Clubs), c(deck.King, deck.Clubs), c(deck.Ace, deck.Clubs),
		}
		melds, dw, pts := BestMeldSplit(hand)
		assert.Empty(t, melds)
		assert.Len(t, dw, 3)
		assert.Equal(t, 21, pts)
	})
}

func TestDeadwoodPoints(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 1, deadwoodPoints(c(deck.Ace, deck.Spades)))
	assert.Equal(t, 5, deadwoodPoints(c(deck.Five, deck.Hearts)))
	assert.Equal(t, 10, deadwoodPoints(c(deck.Ten, deck.Clubs)))
	assert.Equal(t, 10, deadwoodPoints(c(deck.King, deck.Diamonds)))
}
