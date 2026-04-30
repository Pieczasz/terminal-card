// Package deck contains implementation for card (suit and rank) as well as deck builder funcitonality
// shuffling and deck manipulation methods.
package deck

import (
	"math/rand/v2"
)

type Deck struct {
	cards []*Card
}

func New(cards []*Card) *Deck {
	deck := &Deck{
		cards: cards,
	}
	deck.Shuffle()

	return deck
}

func (d *Deck) Shuffle() {
	rand.Shuffle(len(d.cards), func(i, j int) {
		d.cards[i], d.cards[j] = d.cards[j], d.cards[i]
	})
}

func (d *Deck) CheckTop() *Card {
	if len(d.cards) < 1 {
		return nil
	}
	return d.cards[0]
}

func (d *Deck) PickTop() *Card {
	if len(d.cards) < 1 {
		return nil
	}
	topCard := d.cards[0]
	d.cards = d.cards[1:]
	return topCard
}

func (d *Deck) AddCards(cards []*Card) {
	d.cards = append(d.cards, cards...)
}
