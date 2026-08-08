package components

import (
	"testing"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The cache key collapses the whole palette to Theme.Dark.
func TestRenderCard_CacheKeyCoversTheWholePalette(t *testing.T) {
	t.Parallel()

	card := deck.Card{Rank: deck.Queen, Suit: deck.Hearts}

	dark := RenderCard(styles.NewTheme(true), card, false)
	light := RenderCard(styles.NewTheme(false), card, false)
	assert.NotEqual(t, dark, light, "the two modes must not share a cache entry")

	// A second Theme of the same mode has to be interchangeable with the first, which
	// is exactly what the cache assumes when it serves an entry built from another.
	assert.Equal(t, dark, RenderCard(styles.NewTheme(true), card, false))
	assert.Equal(t, light, RenderCard(styles.NewTheme(false), card, false))
}

// Selection is part of the key because it changes the border and the margins.
func TestRenderCard_SelectionIsNotCachedOverTheUnselectedCard(t *testing.T) {
	t.Parallel()
	theme := styles.NewTheme(true)
	card := deck.Card{Rank: deck.Ace, Suit: deck.Spades}

	assert.NotEqual(t, RenderCard(theme, card, false), RenderCard(theme, card, true),
		"a selected card is lifted and recoloured, so it cannot reuse the plain one")
}

// Every rank and suit has to survive the cache: a key collision would silently draw the
// wrong card, which at a poker table is the worst possible bug.
func TestRenderCard_EveryCardIsDistinct(t *testing.T) {
	t.Parallel()
	theme := styles.NewTheme(true)

	seen := make(map[string]deck.Card)
	for _, c := range deck.StandardDeck() {
		rendered := RenderCard(theme, c, false)
		if previous, clash := seen[rendered]; clash {
			t.Fatalf("%v and %v render identically", previous, c)
		}
		seen[rendered] = c
	}
	require.Len(t, seen, len(deck.StandardDeck()))
}

// The cache must return the same string on the miss that fills it and every hit after, or
// the first player to see a card would see a different one from everyone who follows.
func TestRenderCard_HitMatchesMiss(t *testing.T) {
	t.Parallel()
	theme := styles.NewTheme(true)
	card := deck.Card{Rank: deck.Seven, Suit: deck.Clubs}

	first := RenderCard(theme, card, false)
	for range 5 {
		assert.Equal(t, first, RenderCard(theme, card, false))
	}
	assert.Equal(t, renderCard(theme, card, false), first, "the cached value is what the renderer produced")
}
