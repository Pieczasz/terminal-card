package uno

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

func (r *Rules) MinPlayers() int { return 2 }
func (r *Rules) MaxPlayers() int { return 10 }

func (r *Rules) InitialDeck() []deck.Card { return InitialDeck() }
func (r *Rules) InitialDealCount() int    { return 7 }

func (r *Rules) TimeoutAction(_ *game.State) game.Action {
	return ActionDrawCard{}
}

func (r *Rules) OnGameStart(state *game.State) error {
	extra := &State{Direction: 1}
	state.Extra = extra
	state.Discard = deck.New([]deck.Card{})

	// Official Uno never starts on a Wild; redraw until a colored card surfaces.
	var setAside []deck.Card
	for {
		card, ok := state.Deck.Draw()
		if !ok {
			state.Deck.AddCard(setAside...)
			return errors.New("not enough cards to start")
		}
		if !isWild(card.Rank) {
			state.Discard.AddCard(card)
			extra.CurrentColor = card.Suit
			break
		}
		setAside = append(setAside, card)
	}
	if len(setAside) > 0 {
		state.Deck.AddCard(setAside...)
		if err := state.Deck.Shuffle(); err != nil {
			return fmt.Errorf("reshuffle wilds: %w", err)
		}
	}

	top, ok := state.Discard.Peek()
	if !ok {
		return errors.New("no opening card on the discard pile")
	}
	r.applyOpeningCard(state, extra, top)
	return nil
}

// applyOpeningCard plays the action printed on the card that starts the discard
// pile. Nobody played it, so the effect lands on the seat the engine put on turn:
// they lose the turn, draw, or find the table already running the other way. The
// opening card is never a Wild, so there is no colour to choose.
func (r *Rules) applyOpeningCard(state *game.State, extra *State, card deck.Card) {
	first := state.CurrentTurn
	switch card.Rank {
	case Skip:
		first = r.advance(state, extra, 1)
	case DrawTwo:
		if !drawCardsInto(state, state.CurrentTurn, 2) {
			extra.Passes++
		}
		first = r.advance(state, extra, 1)
	case Reverse:
		if len(state.Players) == 2 {
			// Heads-up a Reverse is a Skip, exactly as it is mid-hand.
			first = r.advance(state, extra, 1)
			break
		}
		extra.Direction = -1
	}
	state.CurrentTurn = first
	state.OverrideNextTurn = &first
}

type ActionPlayCard struct {
	Card        deck.Card
	ChosenColor deck.Suit // required for Wild/WildDrawFour, ignored otherwise
}

func (a ActionPlayCard) Name() string { return "uno.PlayCard" }

type ActionDrawCard struct{}

func (a ActionDrawCard) Name() string { return "uno.DrawCard" }

func (r *Rules) ValidateAction(state *game.State, action game.Action) error {
	extra, ok := state.Extra.(*State)
	if !ok {
		return errors.New("invalid state type")
	}
	topCard, ok := state.Discard.Peek()
	if !ok {
		return errors.New("no cards in discard")
	}

	switch a := action.(type) {
	case ActionPlayCard:
		hand := state.Players[state.CurrentTurn].Cards
		if !slices.Contains(hand, a.Card) {
			return errors.New("you don't have that card")
		}
		if isWild(a.Card.Rank) {
			if !validColor(a.ChosenColor) {
				return errors.New("must choose a valid color")
			}
			// A Wild Draw Four is the one card the official rules gate on the hand
			// behind it: it may only be played by someone with nothing of the
			// current colour to play instead.
			if a.Card.Rank == WildDrawFour && hasColor(hand, extra.CurrentColor) {
				return errors.New("wild draw four needs a hand with no card of the current color")
			}
			return nil
		}
		if a.Card.Suit == extra.CurrentColor {
			return nil
		}
		if a.Card.Rank == topCard.Rank && !isWild(topCard.Rank) {
			return nil
		}
		return errors.New("card doesn't match color, number, or symbol")

	case ActionDrawCard:
		return nil
	}
	return errors.New("unknown action")
}

func (r *Rules) ApplyAction(state *game.State, action game.Action) {
	extra, ok := state.Extra.(*State)
	if !ok {
		return
	}
	switch a := action.(type) {
	case ActionPlayCard:
		actor := state.Players[state.CurrentTurn]
		actor.Cards = deck.RemoveOne(actor.Cards, a.Card)
		state.Discard.AddCard(a.Card)
		extra.Passes = 0

		if isWild(a.Card.Rank) {
			extra.CurrentColor = a.ChosenColor
		} else {
			extra.CurrentColor = a.Card.Suit
		}

		switch a.Card.Rank {
		case Skip:
			next := r.advance(state, extra, 2)
			state.OverrideNextTurn = &next
		case Reverse:
			r.applyReverse(state, extra)
		case DrawTwo:
			r.applyForcedDraw(state, extra, 2)
		case WildDrawFour:
			r.applyForcedDraw(state, extra, 4)
		default:
			next := r.advance(state, extra, 1)
			state.OverrideNextTurn = &next
		}
	case ActionDrawCard:
		r.applyVoluntaryDraw(state, extra)
	}
}

// Every action sets OverrideNextTurn: the engine's own advance only steps +1, so a
// reversed table would otherwise ignore Direction.
func (r *Rules) advance(state *game.State, extra *State, steps int) int {
	n := len(state.Players)
	if n == 0 {
		return 0
	}
	delta := int(extra.Direction) * steps
	return ((state.CurrentTurn+delta)%n + n) % n
}

func (r *Rules) applyReverse(state *game.State, extra *State) {
	if len(state.Players) == 2 {
		// Reverse in 2-player acts as Skip (same seat again).
		next := state.CurrentTurn
		state.OverrideNextTurn = &next
		return
	}
	extra.Direction *= -1
	next := r.advance(state, extra, 1)
	state.OverrideNextTurn = &next
}

func (r *Rules) applyForcedDraw(state *game.State, extra *State, n int) {
	victim := r.advance(state, extra, 1)
	if !drawCardsInto(state, victim, n) {
		extra.Passes++
	}
	next := r.advance(state, extra, 2)
	state.OverrideNextTurn = &next
}

func (r *Rules) applyVoluntaryDraw(state *game.State, extra *State) {
	actor := state.CurrentTurn
	if !drawCardsInto(state, actor, 1) {
		extra.Passes++
	} else {
		extra.Passes = 0
	}
	next := r.advance(state, extra, 1)
	state.OverrideNextTurn = &next
}

// drawCardsInto draws up to n cards into the seat, reshuffling discard→stock
// when needed. Returns false only when zero cards were drawn (deadlock).
func drawCardsInto(state *game.State, playerIdx, n int) bool {
	if playerIdx < 0 || playerIdx >= len(state.Players) || n <= 0 {
		return false
	}
	p := state.Players[playerIdx]
	drew := 0
	for range n {
		if state.Deck.IsEmpty() {
			if err := reshuffleDiscardIntoDeck(state); err != nil {
				slog.Error("uno reshuffle failed", "error", err)
				break
			}
		}
		card, ok := state.Deck.Draw()
		if !ok {
			break
		}
		p.Cards = append(p.Cards, card)
		drew++
	}
	return drew > 0
}

func reshuffleDiscardIntoDeck(state *game.State) error {
	top, ok := state.Discard.Draw()
	if !ok {
		return nil
	}
	rest := state.Discard.Cards()
	state.Discard = deck.New([]deck.Card{top})
	state.Deck.AddCard(rest...)
	if err := state.Deck.Shuffle(); err != nil {
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
	if extra, ok := state.Extra.(*State); ok && len(state.Players) > 0 && extra.Passes >= len(state.Players) {
		return true
	}
	return false
}

func (r *Rules) OnPlayerLeave(state *game.State, playerID string) {
	// Passes counts turns a draw yielded nothing on, and the returned hand refills
	// the stock, so the count is stale. Left alone it would also be measured against
	// a table one seat smaller and read as a deadlock that never happened.
	if extra, ok := state.Extra.(*State); ok {
		extra.Passes = 0
	}
	for _, p := range state.Players {
		if p.ID == playerID {
			state.Deck.AddCard(p.Cards...)
			p.Cards = nil
			if err := state.Deck.Shuffle(); err != nil {
				slog.Error("uno shuffle after leave failed", "error", err, "player_id", playerID)
			}
			return
		}
	}
}

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
