package game

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/Pieczasz/terminal-card/internal/deck"
)

// Internal: reaching it is a bug in the caller, not a state a game recovers from.
var errStockNotEmpty = errors.New("stock is not empty")

// ReshuffleDiscardIntoStock moves the discard pile, except the card in play, back into
// an empty stock and shuffles, conserving every card. On shuffle failure the discard is
// restored and the stock dropped, so an unshuffled order can never reach play; that
// only conserves cards while the stock was empty, hence the refusal above.
func ReshuffleDiscardIntoStock(state *State) error {
	return reshuffleDiscardIntoStock(state, (*deck.Pile).Shuffle)
}

// shuffle is a parameter so the failure path is reachable without breaking crypto/rand.
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

// Top card last: Peek and Draw read the end of the pile, so the other order rotates the
// discard and leaves a card nobody played sitting in play.
func restoreDiscard(rest []deck.Card, top deck.Card) *deck.Pile {
	return deck.New(append(rest, top))
}

// ReturnHandToStock keeps the deck whole when a player leaves, reshuffling so the cards
// they were seen holding are not the next ones dealt.
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

// HandEmptyOrAllPassed is the shedding-game win check: a hand is out, or every seat in
// succession could not draw, which is a board with no legal move left rather than a loop.
func HandEmptyOrAllPassed(state *State, passes int) bool {
	for _, p := range state.Players {
		if p != nil && len(p.Cards) == 0 {
			return true
		}
	}
	return len(state.Players) > 0 && passes >= len(state.Players)
}
