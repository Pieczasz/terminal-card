package ginrummy

import (
	"testing"

	"github.com/Pieczasz/terminal-card/internal/deck"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyLayoffs_ExtendRunEnds(t *testing.T) {
	t.Parallel()
	melds := [][]deck.Card{{
		c(deck.Four, deck.Hearts), c(deck.Five, deck.Hearts), c(deck.Six, deck.Hearts),
	}}
	t.Run("low end", func(t *testing.T) {
		t.Parallel()
		ext, rem := ApplyLayoffs([]deck.Card{c(deck.Three, deck.Hearts)}, melds)
		assert.Empty(t, rem)
		require.Len(t, ext[0], 4)
	})
	t.Run("high end", func(t *testing.T) {
		t.Parallel()
		ext, rem := ApplyLayoffs([]deck.Card{c(deck.Seven, deck.Hearts)}, melds)
		assert.Empty(t, rem)
		require.Len(t, ext[0], 4)
	})
}

func TestApplyLayoffs_ExtendSet(t *testing.T) {
	t.Parallel()
	melds := [][]deck.Card{{
		c(deck.King, deck.Spades), c(deck.King, deck.Hearts), c(deck.King, deck.Diamonds),
	}}
	ext, rem := ApplyLayoffs([]deck.Card{c(deck.King, deck.Clubs)}, melds)
	assert.Empty(t, rem)
	assert.Len(t, ext[0], 4)
}

func TestApplyLayoffs_SetAtFourBlocks(t *testing.T) {
	t.Parallel()
	melds := [][]deck.Card{{
		c(deck.Ten, deck.Spades), c(deck.Ten, deck.Hearts),
		c(deck.Ten, deck.Diamonds), c(deck.Ten, deck.Clubs),
	}}
	ext, rem := ApplyLayoffs([]deck.Card{c(deck.Nine, deck.Spades)}, melds)
	assert.Equal(t, melds, ext)
	assert.Len(t, rem, 1)
}

func TestApplyLayoffs_MultiPass(t *testing.T) {
	t.Parallel()
	// 5♥ attaches first; then 4♥ becomes legal on the extended run.
	melds := [][]deck.Card{{
		c(deck.Six, deck.Hearts), c(deck.Seven, deck.Hearts), c(deck.Eight, deck.Hearts),
	}}
	dead := []deck.Card{c(deck.Four, deck.Hearts), c(deck.Five, deck.Hearts)}
	ext, rem := ApplyLayoffs(dead, melds)
	assert.Empty(t, rem)
	assert.Len(t, ext[0], 5)
}

func TestApplyLayoffs_NoneEligible(t *testing.T) {
	t.Parallel()
	melds := [][]deck.Card{{
		c(deck.Two, deck.Clubs), c(deck.Three, deck.Clubs), c(deck.Four, deck.Clubs),
	}}
	dead := []deck.Card{c(deck.Ace, deck.Hearts), c(deck.King, deck.Spades)}
	ext, rem := ApplyLayoffs(dead, melds)
	assert.Equal(t, melds, ext)
	assert.Equal(t, dead, rem)
}

func TestApplyLayoffs_AttachesToExactlyOne(t *testing.T) {
	t.Parallel()
	melds := [][]deck.Card{
		{c(deck.Five, deck.Spades), c(deck.Five, deck.Hearts), c(deck.Five, deck.Diamonds)},
		{c(deck.Three, deck.Clubs), c(deck.Four, deck.Clubs), c(deck.Five, deck.Clubs)},
	}
	// Five of clubs can extend either the set or the run — attaches to first match.
	ext, rem := ApplyLayoffs([]deck.Card{c(deck.Five, deck.Clubs)}, melds)
	assert.Empty(t, rem)
	assert.Len(t, ext[0], 4)
	assert.Len(t, ext[1], 3)
}
