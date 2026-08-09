package crazyeight

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
)

type Rules struct{}

var (
	_ game.Rules              = (*Rules)(nil)
	_ game.PlayerLeaveHandler = (*Rules)(nil)
	_ game.TurnTimeoutHandler = (*Rules)(nil)
)

// TimeoutAction draws. ValidateAction always accepts a draw, and on an exhausted
// board it degrades into the forced pass the turn loop already handles - so it is
// the one move that cannot fail. Picking a card to play would also spend a card the
// player may have been holding for a reason.
func (r *Rules) TimeoutAction(_ *game.State) game.Action {
	return ActionDrawCard{}
}

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

func validSuit(s deck.Suit) bool {
	switch s {
	case deck.Spades, deck.Hearts, deck.Diamonds, deck.Clubs:
		return true
	default:
		return false
	}
}

func (r *Rules) ValidateAction(state *game.State, action game.Action) error {
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
		// Always legal: with cards available it draws, otherwise it is a forced
		// pass so an exhausted board can never soft-lock the turn loop.
		return nil
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

		p.Cards = deck.RemoveOne(p.Cards, card)
		state.Discard.AddCard(card)

		if card.Rank != deck.Eight {
			extra.CurrentSuit = card.Suit
		} else if action.Suit != deck.NoSuit {
			extra.CurrentSuit = action.Suit
		}
		extra.Passes = 0

	case ActionDrawCard:
		if state.Deck.IsEmpty() {
			if err := reshuffleDiscardIntoDeck(state); err != nil {
				slog.Error("crazy eights reshuffle failed", "error", err)
				extra.Passes++ // fail closed: do not draw from an untrusted order
				return
			}
		}
		drawn, ok := state.Deck.Draw()
		if !ok {
			extra.Passes++ // stock and discard exhausted: this turn is a forced pass
			return
		}
		p.Cards = append(p.Cards, drawn)
		extra.Passes = 0
	}
}

// reshuffleDiscardIntoDeck moves the discard pile (except its top card) back
// into the stock and shuffles, conserving every card. On shuffle failure the
// discard is restored so the stock stays empty and the caller can fail closed.
func reshuffleDiscardIntoDeck(state *game.State) error {
	top, ok := state.Discard.Draw()
	if !ok {
		return nil
	}
	rest := state.Discard.Cards()
	state.Discard = deck.New([]deck.Card{top})
	state.Deck.AddCard(rest...)
	if err := state.Deck.Shuffle(); err != nil {
		// Restore prior piles so we never leave an unshuffled stock in play.
		state.Discard = restoreDiscard(rest, top)
		state.Deck = deck.New(nil)
		return fmt.Errorf("shuffle stock after reshuffling discard: %w", err)
	}
	return nil
}

// restoreDiscard puts the pile back the way it was found, top card last. Peek and
// Draw read the end of the pile, so rebuilding it the other way round rotates the
// discard and leaves a card nobody played sitting in play.
func restoreDiscard(rest []deck.Card, top deck.Card) *deck.Pile {
	return deck.New(append(rest, top))
}

func (r *Rules) AfterAction(_ *game.State, _ game.Action) error {
	return nil
}

func (r *Rules) CheckWinCondition(state *game.State) bool {
	for _, p := range state.Players {
		if len(p.Cards) == 0 {
			return true
		}
	}
	// Every player passed in succession: the board is exhausted with no legal
	// move, so the hand is deadlocked and ends (Standings ranks by fewest cards).
	if extra, ok := state.Extra.(*State); ok && len(state.Players) > 0 && extra.Passes >= len(state.Players) {
		return true
	}
	return false
}

// OnPlayerLeave returns the departing player's cards to the stock so the deck
// stays whole; the engine removes the player afterward.
func (r *Rules) OnPlayerLeave(state *game.State, playerID string) {
	// Passes counts turns nobody could draw on, and the returned hand refills the
	// stock, so the count is stale. Left alone it would also be measured against a
	// table one seat smaller and read as a deadlock that never happened.
	if extra, ok := state.Extra.(*State); ok {
		extra.Passes = 0
	}
	for _, p := range state.Players {
		if p.ID == playerID {
			state.Deck.AddCard(p.Cards...)
			p.Cards = nil
			if err := state.Deck.Shuffle(); err != nil {
				slog.Error("crazy eights shuffle after leave failed", "error", err, "player_id", playerID)
			}
			return
		}
	}
}

// AfterPlayerRemoved is a no-op; the engine's generic cursor handling suffices.
func (r *Rules) AfterPlayerRemoved(_ *game.State, _ int) {}

func (r *Rules) Standings(state *game.State) []*game.Player {
	standings := slices.Clone(state.Players)

	slices.SortStableFunc(standings, func(a, b *game.Player) int {
		return len(a.Cards) - len(b.Cards)
	})

	return standings
}

// StandingScore is the value Standings sorted by, so two players left holding the
// same number of cards are reported as the draw they are.
func (r *Rules) StandingScore(_ *game.State, p *game.Player) int {
	return len(p.Cards)
}
