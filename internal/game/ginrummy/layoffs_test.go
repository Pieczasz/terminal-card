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
		ext, rem, laid := applyLayoffs([]deck.Card{c(deck.Three, deck.Hearts)}, melds)
		assert.Empty(t, rem)
		require.Len(t, ext[0], 4)
		assert.Equal(t, []deck.Card{c(deck.Three, deck.Hearts)}, laid)
	})
	t.Run("high end", func(t *testing.T) {
		t.Parallel()
		ext, rem, laid := applyLayoffs([]deck.Card{c(deck.Seven, deck.Hearts)}, melds)
		assert.Empty(t, rem)
		require.Len(t, ext[0], 4)
		assert.Equal(t, []deck.Card{c(deck.Seven, deck.Hearts)}, laid)
	})
}

func TestApplyLayoffs_ExtendSet(t *testing.T) {
	t.Parallel()
	melds := [][]deck.Card{{
		c(deck.King, deck.Spades), c(deck.King, deck.Hearts), c(deck.King, deck.Diamonds),
	}}
	ext, rem, laid := applyLayoffs([]deck.Card{c(deck.King, deck.Clubs)}, melds)
	assert.Empty(t, rem)
	assert.Len(t, ext[0], 4)
	assert.Equal(t, []deck.Card{c(deck.King, deck.Clubs)}, laid)
}

func TestApplyLayoffs_SetAtFourBlocks(t *testing.T) {
	t.Parallel()
	melds := [][]deck.Card{{
		c(deck.Ten, deck.Spades), c(deck.Ten, deck.Hearts),
		c(deck.Ten, deck.Diamonds), c(deck.Ten, deck.Clubs),
	}}
	ext, rem, laid := applyLayoffs([]deck.Card{c(deck.Nine, deck.Spades)}, melds)
	assert.Equal(t, melds, ext)
	assert.Len(t, rem, 1)
	assert.Empty(t, laid)
}

func TestApplyLayoffs_MultiPass(t *testing.T) {
	t.Parallel()
	// 5♥ attaches first; then 4♥ becomes legal on the extended run.
	melds := [][]deck.Card{{
		c(deck.Six, deck.Hearts), c(deck.Seven, deck.Hearts), c(deck.Eight, deck.Hearts),
	}}
	dead := []deck.Card{c(deck.Four, deck.Hearts), c(deck.Five, deck.Hearts)}
	ext, rem, laid := applyLayoffs(dead, melds)
	assert.Empty(t, rem)
	assert.Len(t, ext[0], 5)
	assert.ElementsMatch(t, dead, laid, "both cards moved, and the loop knows which")
}

func TestApplyLayoffs_NoneEligible(t *testing.T) {
	t.Parallel()
	melds := [][]deck.Card{{
		c(deck.Two, deck.Clubs), c(deck.Three, deck.Clubs), c(deck.Four, deck.Clubs),
	}}
	dead := []deck.Card{c(deck.Ace, deck.Hearts), c(deck.King, deck.Spades)}
	ext, rem, laid := applyLayoffs(dead, melds)
	assert.Equal(t, melds, ext)
	assert.Equal(t, dead, rem)
	assert.Empty(t, laid)
}

// Runs before sets, and the fixture has to make the two choices score differently or
// it proves nothing. The knocker holds a set of eights and a spade run: 8♠ fits both,
// but only the run leaves 9♠ somewhere to go.
func TestApplyLayoffs_RunsBeforeSets(t *testing.T) {
	t.Parallel()
	melds := [][]deck.Card{
		{c(deck.Eight, deck.Hearts), c(deck.Eight, deck.Diamonds), c(deck.Eight, deck.Clubs)},
		{c(deck.Five, deck.Spades), c(deck.Six, deck.Spades), c(deck.Seven, deck.Spades)},
	}
	dead := []deck.Card{c(deck.Eight, deck.Spades), c(deck.Nine, deck.Spades)}

	ext, rem, laid := applyLayoffs(dead, melds)

	assert.Empty(t, rem, "spending 8♠ on the set strands 9♠")
	assert.ElementsMatch(t, dead, laid)
	assert.Len(t, ext[0], 3, "the set is untouched")
	assert.Len(t, ext[1], 5, "the run absorbed both cards")
}
