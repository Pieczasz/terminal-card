package crazyeight

import (
	"errors"
	"log/slog"
	"slices"

	"terminalcard/internal/deck"
	"terminalcard/internal/game"
	"terminalcard/internal/player"
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

func (r *CrazyEightsRules) OnGameStart(state *game.State) error {
	extra := &State{CurrentSuit: deck.NoSuit}
	state.Extra = extra
	state.Discard = deck.New([]deck.Card{})

	firstCard, ok := state.Deck.Draw()
	if !ok {
		return errors.New("not enough cards to start the game")
	}
	state.Discard.AddCard(firstCard)
	extra.CurrentSuit = firstCard.Suit

	return nil
}

type ActionPlayCard struct {
	Cards []deck.Card
	Suit  deck.Suit
}

func (a ActionPlayCard) Name() string { return "crazyeight.PlayCard" }

type ActionDrawCard struct{}

func (a ActionDrawCard) Name() string { return "crazyeight.DrawCard" }

type ActionPickSuit struct {
	Suit deck.Suit
}

func (a ActionPickSuit) Name() string { return "crazyeight.PickSuit" }

func validSuit(s deck.Suit) bool {
	switch s {
	case deck.Spades, deck.Hearts, deck.Diamonds, deck.Clubs:
		return true
	default:
		return false
	}
}

func (r *CrazyEightsRules) PreActionCondition(state *game.State, action game.Action) error {
	topCard, ok := state.Discard.Peek()
	if !ok {
		return errors.New("no cards in discard pile")
	}

	extra, ok := state.Extra.(*State)
	if !ok {
		return errors.New("invalid state type")
	}

	switch action := action.(type) {
	case ActionPlayCard:
		if len(action.Cards) != 1 {
			return errors.New("must play exactly one card")
		}
		card := action.Cards[0]

		if !slices.Contains(state.Players[state.CurrentTurn].Cards, card) {
			slog.Warn("illegal play attempted", "player_id", state.Players[state.CurrentTurn].ID)
			return errors.New("you don't have that card")
		}

		if card.Rank == deck.Eight {
			if action.Suit != deck.NoSuit && !validSuit(action.Suit) {
				return errors.New("invalid suit")
			}
			return nil
		}
		if card.Suit == extra.CurrentSuit {
			return nil
		}
		if card.Rank == topCard.Rank {
			return nil
		}
		return errors.New("card doesn't match top discard")

	case ActionDrawCard:
		if state.Deck.IsEmpty() {
			return errors.New("deck is empty")
		}
		return nil

	case ActionPickSuit:
		// Suit changes must go through ActionPlayCard when playing an eight.
		return errors.New("pick suit is not a standalone action")
	}

	return errors.New("unknown action")
}

func (r *CrazyEightsRules) ApplyAction(state *game.State, action game.Action) {
	extra, ok := state.Extra.(*State)
	if !ok {
		return
	}
	p := state.Players[state.CurrentTurn]

	switch action := action.(type) {
	case ActionPlayCard:
		card := action.Cards[0]

		newHand := make([]deck.Card, 0, len(p.Cards)-1)
		removed := false
		for _, c := range p.Cards {
			if c == card && !removed {
				removed = true
				continue
			}
			newHand = append(newHand, c)
		}
		p.Cards = newHand

		state.Discard.AddCard(card)

		if card.Rank != deck.Eight {
			extra.CurrentSuit = card.Suit
		} else if action.Suit != deck.NoSuit {
			extra.CurrentSuit = action.Suit
		}

	case ActionDrawCard:
		drawn, ok := state.Deck.Draw()
		if !ok {
			return
		}
		p.Cards = append(p.Cards, drawn)
	}
}

func (r *CrazyEightsRules) PostActionCondition(_ *game.State, _ game.Action) error {
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

func (r *CrazyEightsRules) GetStandings(state *game.State) []*player.Player {
	standings := make([]*player.Player, len(state.Players))
	copy(standings, state.Players)

	slices.SortStableFunc(standings, func(a, b *player.Player) int {
		return len(a.Cards) - len(b.Cards)
	})

	return standings
}
