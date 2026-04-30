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
	return p.cards[0], true
}

func (p *Pile) Draw() (Card, bool) {
	if len(p.cards) < 1 {
		return Card{}, false
	}

	topCard := p.cards[0]
	p.cards = p.cards[1:]
	return topCard, true
}

func (p *Pile) DrawNCards(cardsToDraw int) ([]Card, bool) {
	if cardsToDraw > len(p.cards) {
		return nil, false
	}

	nCards := p.cards[0:cardsToDraw]
	p.cards = p.cards[cardsToDraw+1:]
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
