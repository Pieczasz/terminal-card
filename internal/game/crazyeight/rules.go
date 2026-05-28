package crazyeight

import (
	"client/internal/deck"
	"client/internal/game"
	"errors"
	"slices"
)

type CrazyEightsRules struct{}

var _ game.Rules = (*CrazyEightsRules)(nil)

func (r *CrazyEightsRules) Name() string    { return "Crazy Eights" }
func (r *CrazyEightsRules) MinPlayers() int { return 2 }
func (r *CrazyEightsRules) MaxPlayers() int { return 6 }

func (r *CrazyEightsRules) InitialDeck() []deck.Card {
	return deck.StandardDeck()
}

func (r *CrazyEightsRules) InitialDealCount() int {
	return 7
}

func (r *CrazyEightsRules) PreActionCondition(state *game.State, action game.Action) error {
	topCard, ok := state.Discard.Peak()
	if !ok {
		return errors.New("no cards in discard pile")
	}

	extra, ok := state.Extra.(*State)
	if !ok {
		return errors.New("invalid state type")
	}

	switch action.Type {
	case game.ActionPlayCard:
		if len(action.Cards) != 1 {
			return errors.New("must play exactly one card")
		}
		card := action.Cards[0]

		if !slices.Contains(state.Players[state.CurrentTurn].Cards, card) {
			// TODO: log it? someone is cheating?
			return errors.New("you don't have that card")
		}

		if card.Rank == deck.Eight {
			return nil
		}
		if card.Suit == extra.CurrentSuit {
			return nil
		}
		if card.Rank == topCard.Rank {
			return nil
		}
		return errors.New("card doesn't match top discard")

	case game.ActionDrawCard:
		if state.Deck.IsEmpty() {
			return errors.New("deck is empty")
		}
		return nil

	case game.ActionPickSuit:
		return nil
	}

	return errors.New("unknown action")
}

func (r *CrazyEightsRules) ApplyAction(state *game.State, action game.Action) {
	extra := state.Extra.(*State)
	player := state.Players[state.CurrentTurn]

	switch action.Type {
	case game.ActionPlayCard:
		card := action.Cards[0]

		newHand := make([]deck.Card, 0, len(player.Cards)-1)
		removed := false
		for _, c := range player.Cards {
			if c == card && !removed {
				removed = true
				continue
			}
			newHand = append(newHand, c)
		}
		player.Cards = newHand

		state.Discard.AddCard(card)

		if card.Rank != deck.Eight {
			extra.CurrentSuit = card.Suit
		}

	case game.ActionDrawCard:
		drawn, _ := state.Deck.Draw()
		player.Cards = append(player.Cards, drawn)

	case game.ActionPickSuit:
		extra.CurrentSuit = action.Suit
	}
}

func (r *CrazyEightsRules) PostActionCondition(state *game.State, action game.Action) error {
	return nil
}

func (r *CrazyEightsRules) CheckWinCondition(state *game.State) bool {
	for _, p := range state.Players {
		if len(p.Cards) == 0 {
			return true
		}
	}
	return false
}
