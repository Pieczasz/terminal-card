package game

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/Pieczasz/terminal-card/internal/deck"
)

// errStockNotEmpty guards ReshuffleDiscardIntoStock's precondition. It is internal
// because no caller can do anything about it: reaching it is a bug in the caller,
// not a state a game recovers from.
var errStockNotEmpty = errors.New("stock is not empty")

// ReshuffleDiscardIntoStock moves the discard pile, except the card in play, back
// into the stock and shuffles, conserving every card. The shedding games (uno, crazy
// eights) both refill an exhausted stock this way, and two copies of it is two
// chances to leak a card.
//
// The stock must be empty. On shuffle failure the discard is restored and the stock
// dropped so an unshuffled order can never reach play - and dropping it only
// conserves cards while there was nothing in it, which is why a non-empty stock is
// refused rather than quietly destroyed.
func ReshuffleDiscardIntoStock(state *State) error {
	return reshuffleDiscardIntoStock(state, (*deck.Pile).Shuffle)
}

// reshuffleDiscardIntoStock takes the shuffle as an argument purely so the failure
// path - the one branch that has to conserve cards while throwing a pile away - is
// reachable from a test without a broken crypto/rand.
func reshuffleDiscardIntoStock(state *State, shuffle func(*deck.Pile) error) error {
	if !state.Deck.IsEmpty() {
		return errStockNotEmpty
	}
	top, ok := state.Discard.Draw()
	if !ok {
		return nil
	}
	rest := state.Discard.Cards()
	state.Discard = deck.New([]deck.Card{top})
	state.Deck.AddCard(rest...)
	if err := shuffle(state.Deck); err != nil {
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

// ReturnHandToStock hands a leaving player's cards back to the stock so the deck
// stays whole, and reshuffles so the cards they were seen holding are not the next
// ones dealt. gameName only names the game in the failure log.
func ReturnHandToStock(state *State, playerID, gameName string) {
	for _, p := range state.Players {
		if p == nil || p.ID != playerID {
			continue
		}
		state.Deck.AddCard(p.Cards...)
		p.Cards = nil
		if err := state.Deck.Shuffle(); err != nil {
			slog.Error("shuffle after leave failed",
				"error", err, "game", gameName, "player_id", playerID)
		}
		return
	}
}

// HandEmptyOrAllPassed is the shedding-game win check: somebody has shed their last
// card, or every seat in succession could not draw, which is a board with no legal
// move left and a hand that has to end rather than loop. An empty table is neither.
func HandEmptyOrAllPassed(state *State, passes int) bool {
	for _, p := range state.Players {
		if p != nil && len(p.Cards) == 0 {
			return true
		}
	}
	return len(state.Players) > 0 && passes >= len(state.Players)
}
