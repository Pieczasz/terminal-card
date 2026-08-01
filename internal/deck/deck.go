package deck

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"slices"
)

type Pile struct {
	cards []Card
}

func New(cards []Card) *Pile {
	return &Pile{
		cards: slices.Clone(cards),
	}
}

// Shuffle uses crypto/rand for unpredictable deal order in ranked play.
func (p *Pile) Shuffle() error {
	n := len(p.cards)
	for i := n - 1; i > 0; i-- {
		jBig, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return fmt.Errorf("crypto/rand shuffle: %w", err)
		}
		j := int(jBig.Int64())
		p.cards[i], p.cards[j] = p.cards[j], p.cards[i]
	}
	return nil
}

func (p *Pile) Peek() (Card, bool) {
	if len(p.cards) < 1 {
		return Card{}, false
	}
	return p.cards[len(p.cards)-1], true
}

func (p *Pile) Draw() (Card, bool) {
	if len(p.cards) < 1 {
		return Card{}, false
	}
	lastIdx := len(p.cards) - 1
	topCard := p.cards[lastIdx]
	p.cards = p.cards[:lastIdx]
	return topCard, true
}

func (p *Pile) DrawNCards(cardsToDraw int) ([]Card, bool) {
	if cardsToDraw > len(p.cards) {
		return nil, false
	}

	splitIdx := len(p.cards) - cardsToDraw
	nCards := make([]Card, cardsToDraw)
	copy(nCards, p.cards[splitIdx:])
	p.cards = p.cards[:splitIdx]

	slices.Reverse(nCards)
	return nCards, true
}

func (p *Pile) AddCard(cards ...Card) {
	p.cards = append(p.cards, cards...)
}

func (p *Pile) AddAllCards(cards []Card) {
	p.cards = append(p.cards, cards...)
}

func (p *Pile) Size() int {
	return len(p.cards)
}

func (p *Pile) IsEmpty() bool {
	return len(p.cards) < 1
}

func (p *Pile) Cards() []Card {
	return slices.Clone(p.cards)
}

func (p *Pile) Contains(card Card) bool {
	return slices.Contains(p.cards, card)
}
