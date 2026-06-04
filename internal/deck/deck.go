// Package deck contains implementation for card (suit and rank) as well as deck builder funcitonality
// shuffling and deck manipulation methods.
package deck

import (
	"math/rand/v2"
	"slices"
)

type Pile struct {
	cards []Card
}

func New(cards []Card) *Pile {
	pile := &Pile{
		cards: cards,
	}
	pile.Shuffle()

	return pile
}

func (p *Pile) Shuffle() {
	rand.Shuffle(len(p.cards), func(i, j int) {
		p.cards[i], p.cards[j] = p.cards[j], p.cards[i]
	})
}

func (p *Pile) Peak() (Card, bool) {
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
	return p.cards
}

func (p *Pile) Contains(card Card) bool {
	return slices.Contains(p.cards, card)
}
