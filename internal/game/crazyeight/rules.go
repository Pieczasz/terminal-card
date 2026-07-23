package crazyeight

import (
	"errors"
	"log/slog"
	"slices"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/player"
)

type Rules struct{}

var _ game.Rules = (*Rules)(nil)

func (r *Rules) Name() string    { return "Crazy Eights" }
func (r *Rules) MinPlayers() int { return 2 }
func (r *Rules) MaxPlayers() int { return 6 }

func (r *Rules) InitialDeck() []deck.Card {
	return deck.StandardDeck()
}

func (r *Rules) InitialDealCount() int {
	return 7
}

func (r *Rules) OnGameStart(state *game.State) error {
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

func (r *Rules) PreActionCondition(state *game.State, action game.Action) error {
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
			if !validSuit(action.Suit) {
				return errors.New("must choose a suit when playing an eight")
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
		if state.Deck.IsEmpty() && state.Discard.Size() <= 1 {
			return errors.New("no cards left to draw")
		}
		return nil

	case ActionPickSuit:
		// Suit changes must go through ActionPlayCard when playing an eight.
		return errors.New("pick suit is not a standalone action")
	}

	return errors.New("unknown action")
}

func (r *Rules) ApplyAction(state *game.State, action game.Action) {
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
		if state.Deck.IsEmpty() {
			reshuffleDiscardIntoDeck(state)
		}
		drawn, ok := state.Deck.Draw()
		if !ok {
			return
		}
		p.Cards = append(p.Cards, drawn)
	}
}

// reshuffleDiscardIntoDeck moves the discard pile (except its top card) back
// into the stock and shuffles, conserving every card.
func reshuffleDiscardIntoDeck(state *game.State) {
	top, ok := state.Discard.Draw()
	if !ok {
		return
	}
	rest := state.Discard.Cards()
	state.Discard = deck.New([]deck.Card{top})
	state.Deck.AddCard(rest...)
	_ = state.Deck.Shuffle()
}

func (r *Rules) PostActionCondition(_ *game.State, _ game.Action) error {
	return nil
}

func (r *Rules) CheckWinCondition(state *game.State) bool {
	for _, p := range state.Players {
		if len(p.Cards) == 0 {
			return true
		}
	}
	return false
}

func (r *Rules) GetStandings(state *game.State) []*player.Player {
	standings := make([]*player.Player, len(state.Players))
	copy(standings, state.Players)

	slices.SortStableFunc(standings, func(a, b *player.Player) int {
		return len(a.Cards) - len(b.Cards)
	})

	return standings
}
