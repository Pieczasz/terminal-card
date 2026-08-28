package deck

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A shuffle has to be a permutation and it has to be uniform. Contains-based checks
// see neither: a shuffle that duplicated a card or only ever rotated the pile would
// pass them.
func TestPile_Shuffle_IsAPermutation(t *testing.T) {
	t.Parallel()
	cards := StandardDeck()
	p := New(cards)

	require.NoError(t, p.Shuffle())

	got := p.Cards()
	require.Len(t, got, len(cards))
	sort := func(in []Card) []Card {
		out := slices.Clone(in)
		slices.SortFunc(out, func(a, b Card) int {
			if d := int(a.Suit) - int(b.Suit); d != 0 {
				return d
			}
			return int(a.Rank) - int(b.Rank)
		})
		return out
	}
	assert.Equal(t, sort(cards), sort(got), "the same cards, each exactly once")
}

// Three cards have six orderings, and a biased shuffle shows up as one of them being
// rare. A Fisher-Yates that drew its index from the wrong range is the usual way to
// get this wrong, and it still returns a permutation every time.
func TestPile_Shuffle_HitsEveryPermutation(t *testing.T) {
	t.Parallel()
	const rounds = 30000
	cards := []Card{{Rank: Ace, Suit: Spades}, {Rank: King, Suit: Hearts}, {Rank: Queen, Suit: Diamonds}}

	counts := map[string]int{}
	for range rounds {
		p := New(cards)
		require.NoError(t, p.Shuffle())
		key := fmt.Sprint(p.Cards())
		counts[key]++
	}

	require.Len(t, counts, 6, "every ordering must be reachable, got %v", counts)
	expected := rounds / 6
	for order, n := range counts {
		assert.InEpsilon(t, expected, n, 0.10, "ordering %s came up %d times", order, n)
	}
}

func TestPile_Peek(t *testing.T) {
	t.Parallel()
	cards := []Card{{Rank: Ace, Suit: Spades}}
	p := &Pile{cards: cards}

	gotCard, gotOk := p.Peek()
	assert.True(t, gotOk)
	assert.Equal(t, Card{Rank: Ace, Suit: Spades}, gotCard)

	gotSize := p.Size()
	assert.Equal(t, 1, gotSize)

	pEmpty := &Pile{}
	_, gotEmptyOk := pEmpty.Peek()
	assert.False(t, gotEmptyOk)
}

func TestPile_Draw(t *testing.T) {
	t.Parallel()
	cards := []Card{{Rank: Ace, Suit: Spades}, {Rank: King, Suit: Hearts}}
	p := &Pile{cards: cards}

	gotCard, gotOk := p.Draw()
	assert.True(t, gotOk)
	assert.Equal(t, Card{Rank: King, Suit: Hearts}, gotCard)

	gotSize := p.Size()
	assert.Equal(t, 1, gotSize)

	p.Draw()
	_, gotEmptyOk := p.Draw()
	assert.False(t, gotEmptyOk)
}

func TestPile_DrawNCards(t *testing.T) {
	t.Parallel()
	cards := []Card{{Rank: Ace, Suit: Spades}, {Rank: Two, Suit: Spades}, {Rank: Three, Suit: Spades}}
	p := &Pile{cards: cards}

	gotCards, gotOk := p.DrawNCards(2)
	assert.True(t, gotOk)
	assert.Len(t, gotCards, 2)

	want := []Card{{Rank: Three, Suit: Spades}, {Rank: Two, Suit: Spades}}
	assert.Equal(t, want, gotCards)

	assert.Equal(t, 1, p.Size())

	_, gotOkFail := p.DrawNCards(5)
	assert.False(t, gotOkFail)
}

func TestPile_AddCard(t *testing.T) {
	t.Parallel()
	p := &Pile{}
	p.AddCard(Card{Rank: Ace, Suit: Spades})

	assert.Equal(t, 1, p.Size())
	assert.True(t, p.Contains(Card{Rank: Ace, Suit: Spades}))
}

func FuzzPile_DrawNCards(f *testing.F) {
	f.Add(0, 0)
	f.Add(3, -1)
	f.Add(3, 5)
	f.Add(52, 52)

	f.Fuzz(func(t *testing.T, size, want int) {
		if size < 0 || size > 512 {
			t.Skip()
		}
		cards := make([]Card, 0, size)
		for i := range size {
			cards = append(cards, Card{Rank: Rank(i % 14), Suit: Suit(i % 4)})
		}
		p := New(cards)

		got, ok := p.DrawNCards(want)
		if !ok {
			assert.Empty(t, got, "a refused draw yields no cards")
			assert.Equal(t, size, p.Size(), "a refused draw leaves the pile untouched")
			return
		}
		assert.Len(t, got, want, "a successful draw yields exactly the requested count")
		assert.Equal(t, size-want, p.Size(), "the pile shrinks by exactly what was drawn")
	})
}
