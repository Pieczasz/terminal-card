package uno

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/player"
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
	return nil
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
		if !slices.Contains(state.Players[state.CurrentTurn].Cards, a.Card) {
			return errors.New("you don't have that card")
		}
		if isWild(a.Card.Rank) {
			if !validColor(a.ChosenColor) {
				return errors.New("must choose a valid color")
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
		removeOneMatchingCard(state.Players[state.CurrentTurn], a.Card)
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

// Every action sets OverrideNextTurn: TurnManager.Next only steps +1, so a
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

func removeOneMatchingCard(p *player.Player, card deck.Card) {
	newHand := make([]deck.Card, 0, len(p.Cards))
	removed := false
	for _, c := range p.Cards {
		if c == card && !removed {
			removed = true
			continue
		}
		newHand = append(newHand, c)
	}
	p.Cards = newHand
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
		state.Discard.AddCard(rest...)
		state.Deck = deck.New(nil)
		return fmt.Errorf("shuffle stock after reshuffling discard: %w", err)
	}
	return nil
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

func (r *Rules) Standings(state *game.State) []*player.Player {
	standings := slices.Clone(state.Players)
	slices.SortStableFunc(standings, func(a, b *player.Player) int {
		return len(a.Cards) - len(b.Cards)
	})
	return standings
}
