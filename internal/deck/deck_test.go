package deck

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPile_Shuffle(t *testing.T) {
	t.Parallel()
	cards := []Card{{Rank: Ace, Suit: Spades}, {Rank: King, Suit: Hearts}, {Rank: Queen, Suit: Diamonds}}
	p := New(cards)

	// Shuffle might not change the order every time, but it should contain the same cards
	require.NoError(t, p.Shuffle())

	got := p.Size()
	want := 3
	assert.Equal(t, want, got)

	assert.True(t, p.Contains(Card{Rank: Ace, Suit: Spades}))
	assert.True(t, p.Contains(Card{Rank: King, Suit: Hearts}))
	assert.True(t, p.Contains(Card{Rank: Queen, Suit: Diamonds}))
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

func TestPile_AddAllCards(t *testing.T) {
	t.Parallel()
	p := &Pile{}
	cards := []Card{{Rank: Ace, Suit: Spades}, {Rank: King, Suit: Hearts}}
	p.AddAllCards(cards)

	assert.Equal(t, 2, p.Size())
}

// FuzzPile_DrawNCards guards the arithmetic in DrawNCards: it slices by the caller's
// count, and a negative count used to reach make([]Card, n) and panic the server via
// Engine.Start -> Rules.InitialDealCount.
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
