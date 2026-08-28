package ginrummy

import (
	"slices"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/deck"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
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
	melds, dw, pts := bestMeldSplit(hand)
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
			_, dw, pts := bestMeldSplit(hand)
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
	_, dw, pts := bestMeldSplit(hand)
	assert.Equal(t, 19, pts)
	assert.Len(t, dw, 2)
}

func TestBestMeldSplit_FullyDeadwood(t *testing.T) {
	t.Parallel()
	hand := []deck.Card{
		c(deck.Ace, deck.Spades), c(deck.Three, deck.Hearts), c(deck.Five, deck.Diamonds),
		c(deck.Seven, deck.Clubs), c(deck.Nine, deck.Spades),
	}
	melds, dw, pts := bestMeldSplit(hand)
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
	_, dw, pts := bestMeldSplit(hand)
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
	_, dw, pts := bestMeldSplit(hand)
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
		_, dw, pts := bestMeldSplit(hand)
		assert.Empty(t, dw)
		assert.Equal(t, 0, pts)
	})
	t.Run("Q-K-A invalid", func(t *testing.T) {
		t.Parallel()
		hand := []deck.Card{
			c(deck.Queen, deck.Clubs), c(deck.King, deck.Clubs), c(deck.Ace, deck.Clubs),
		}
		melds, dw, pts := bestMeldSplit(hand)
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

// The classic greedy traps. Taking the highest-scoring meld first, or the longest run
// first, gets both of these wrong, and the difference is the whole knock.
func TestBestMeldSplit_BeatsGreedy(t *testing.T) {
	t.Parallel()

	t.Run("a set that steals a card the run needed", func(t *testing.T) {
		t.Parallel()
		hand := []deck.Card{
			c(deck.Seven, deck.Spades), c(deck.Eight, deck.Spades), c(deck.Nine, deck.Spades),
			c(deck.Seven, deck.Hearts), c(deck.Seven, deck.Diamonds),
		}
		melds, dw, pts := bestMeldSplit(hand)
		assert.Equal(t, 14, pts, "set-first leaves 8♠+9♠ = 17")
		require.Len(t, melds, 1)
		assert.True(t, isRun(melds[0]))
		assert.Len(t, dw, 2)
	})

	t.Run("a run that has to give a card back to a set", func(t *testing.T) {
		t.Parallel()
		hand := []deck.Card{
			c(deck.Four, deck.Spades), c(deck.Five, deck.Spades), c(deck.Six, deck.Spades),
			c(deck.Seven, deck.Spades),
			c(deck.Seven, deck.Hearts), c(deck.Seven, deck.Diamonds),
		}
		melds, dw, pts := bestMeldSplit(hand)
		assert.Equal(t, 0, pts, "the longest run leaves 7♥+7♦ = 14")
		assert.Empty(t, dw)
		assert.Len(t, melds, 2)
	})
}

// The candidate melds are uint16 index masks, so a hand past bit 15 would be scored
// on melds nobody looked at. Unreachable in play; the point is that it fails closed.
func TestBestMeldSplit_OversizedHandIsAllDeadwood(t *testing.T) {
	t.Parallel()
	hand := deck.StandardDeck()[:maskBits+1]

	melds, dw, pts := bestMeldSplit(hand)

	assert.Empty(t, melds, "no meld may be claimed on an unsearchable hand")
	assert.Len(t, dw, len(hand))
	assert.Equal(t, sumDeadwood(hand), pts)
}

// The split is a partition: every card is in exactly one meld or in the deadwood, the
// points match the cards, and every meld is a real one.
func TestBestMeldSplit_IsAPartition(t *testing.T) {
	t.Parallel()
	full := deck.StandardDeck()

	rapid.Check(t, func(rt *rapid.T) {
		size := rapid.IntRange(1, 11).Draw(rt, "size")
		idxs := rapid.SliceOfNDistinct(rapid.IntRange(0, len(full)-1), size, size,
			func(i int) int { return i }).Draw(rt, "cards")
		hand := make([]deck.Card, 0, size)
		for _, i := range idxs {
			hand = append(hand, full[i])
		}

		melds, dw, pts := bestMeldSplit(hand)

		require.Equal(rt, sumDeadwood(dw), pts, "the points must count the deadwood reported")

		seen := make([]deck.Card, 0, size)
		for _, meld := range melds {
			require.True(rt, isSet(meld) || isRun(meld), "not a meld: %v", meld)
			seen = append(seen, meld...)
		}
		counts := map[deck.Card]int{}
		for _, card := range append(slices.Clone(seen), dw...) {
			counts[card]++
			require.Equal(rt, 1, counts[card], "%v is in two places", card)
		}
		require.ElementsMatch(rt, hand, append(slices.Clone(seen), dw...),
			"melds plus deadwood must be exactly the hand")
	})
}
