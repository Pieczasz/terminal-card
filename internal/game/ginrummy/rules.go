package ginrummy

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
)

type Rules struct{}

var (
	_ game.Rules               = (*Rules)(nil)
	_ game.TurnTimeoutHandler  = (*Rules)(nil)
	_ game.TurnDurationHandler = (*Rules)(nil)
	// No PlayerLeaveHandler: gin is strictly 2-player (MinPlayers == MaxPlayers == 2).
	// When a player leaves mid-hand, the match ends immediately via the engine's
	// last-player-standing path. There is no shared state to clean up.
)

type ActionDrawStock struct{}

func (a ActionDrawStock) Name() string { return "ginrummy.DrawStock" }

type ActionDrawDiscard struct{}

func (a ActionDrawDiscard) Name() string { return "ginrummy.DrawDiscard" }

type ActionDiscard struct {
	Card deck.Card
}

func (a ActionDiscard) Name() string { return "ginrummy.Discard" }

type ActionKnock struct {
	Discard deck.Card
}

func (a ActionKnock) Name() string { return "ginrummy.Knock" }

type ActionNextHand struct{}

func (a ActionNextHand) Name() string { return "ginrummy.NextHand" }

func (r *Rules) MinPlayers() int { return 2 }
func (r *Rules) MaxPlayers() int { return 2 }

func (r *Rules) InitialDeck() []deck.Card { return deck.StandardDeck() }

// InitialDealCount is zero: beginHand owns the deal for every hand of the match.
func (r *Rules) InitialDealCount() int { return 0 }

func (r *Rules) OnGameStart(state *game.State) error {
	if len(state.Players) != 2 {
		return fmt.Errorf("gin rummy requires exactly 2 players, got %d", len(state.Players))
	}
	extra := &State{
		FirstActor:       state.CurrentTurn,
		CumulativeScores: make(map[string]int, len(state.Players)),
	}
	for _, p := range state.Players {
		extra.CumulativeScores[p.ID] = 0
	}
	state.Extra = extra
	return r.beginHand(state, extra)
}

func (r *Rules) beginHand(state *game.State, extra *State) error {
	extra.HandNumber++
	extra.HandComplete = false
	extra.LastHandResult = nil
	extra.HandPhase = AwaitingDraw
	extra.TakenUpcard = nil
	extra.TurnsThisHand = 0

	state.Deck = deck.New(deck.StandardDeck())
	if err := state.Deck.Shuffle(); err != nil {
		return fmt.Errorf("shuffle: %w", err)
	}
	for _, p := range state.Players {
		cards, ok := state.Deck.DrawNCards(dealCount)
		if !ok {
			return errors.New("insufficient cards to deal")
		}
		p.Cards = cards
	}
	upCard, ok := state.Deck.Draw()
	if !ok {
		return errors.New("not enough cards to start discard pile")
	}
	state.Discard = deck.New([]deck.Card{upCard})

	// A local, never &extra.FirstActor: the engine holds this pointer until it settles
	// the cursor, and a write to FirstActor in that window would silently redirect the turn.
	first := extra.FirstActor
	state.CurrentTurn = first
	state.OverrideNextTurn = &first
	return nil
}

func (r *Rules) ValidateAction(state *game.State, action game.Action) error {
	extra, ok := state.Extra.(*State)
	if !ok {
		return errors.New("invalid state type")
	}

	if _, isNext := action.(ActionNextHand); isNext {
		if !extra.HandComplete {
			return errors.New("hand is still being played")
		}
		if extra.MatchComplete {
			return errors.New("match is over")
		}
		return nil
	}
	if extra.HandComplete {
		return errors.New("hand is over")
	}

	p := state.Players[state.CurrentTurn]
	switch action := action.(type) {
	case ActionDrawStock:
		if extra.HandPhase != AwaitingDraw {
			return errors.New("must discard first")
		}
		// WallStockSize cards stay undealt: drawing them would strand the hand.
		if state.Deck.Size() <= WallStockSize {
			return errors.New("stock is at the wall")
		}
		return nil
	case ActionDrawDiscard:
		if extra.HandPhase != AwaitingDraw {
			return errors.New("must discard first")
		}
		if _, ok := state.Discard.Peek(); !ok {
			return errors.New("discard pile is empty")
		}
		return nil
	case ActionDiscard:
		if extra.HandPhase != AwaitingDiscard {
			return errors.New("must draw first")
		}
		if !slices.Contains(p.Cards, action.Card) {
			return errors.New("you don't have that card")
		}
		return validateNotTakenUpcard(extra, action.Card)
	case ActionKnock:
		if extra.HandPhase != AwaitingDiscard {
			return errors.New("must draw first")
		}
		if !slices.Contains(p.Cards, action.Discard) {
			return errors.New("you don't have that card")
		}
		if err := validateNotTakenUpcard(extra, action.Discard); err != nil {
			return err
		}
		remaining := deck.RemoveOne(p.Cards, action.Discard)
		_, _, deadwoodPts := BestMeldSplit(remaining)
		if deadwoodPts > KnockThreshold {
			return fmt.Errorf("deadwood %d exceeds limit %d", deadwoodPts, KnockThreshold)
		}
		return nil
	default:
		return errors.New("unknown action")
	}
}

// validateNotTakenUpcard enforces the standard rule that the card just drawn from
// the discard pile cannot be laid straight back. See State.TakenUpcard.
func validateNotTakenUpcard(extra *State, card deck.Card) error {
	if extra.TakenUpcard != nil && *extra.TakenUpcard == card {
		return errors.New("cannot discard the card you just took from the discard pile")
	}
	return nil
}

func (r *Rules) ApplyAction(state *game.State, action game.Action) {
	extra, ok := state.Extra.(*State)
	if !ok {
		return
	}
	p := state.Players[state.CurrentTurn]

	switch action := action.(type) {
	case ActionDrawStock:
		// The phase only moves on a card that actually arrived; AfterAction turns a
		// failed draw into an error rather than letting the player discard a card
		// they never picked up.
		if drawn, ok := state.Deck.Draw(); ok {
			p.Cards = append(p.Cards, drawn)
			extra.TakenUpcard = nil
			extra.HandPhase = AwaitingDiscard
		}
		// Draw then discard is one turn: keep the cursor on this seat.
		cur := state.CurrentTurn
		state.OverrideNextTurn = &cur
	case ActionDrawDiscard:
		if drawn, ok := state.Discard.Draw(); ok {
			p.Cards = append(p.Cards, drawn)
			extra.TakenUpcard = &drawn
			extra.HandPhase = AwaitingDiscard
		}
		cur := state.CurrentTurn
		state.OverrideNextTurn = &cur
	case ActionDiscard:
		p.Cards = deck.RemoveOne(p.Cards, action.Card)
		state.Discard.AddCard(action.Card)
		extra.TakenUpcard = nil
		extra.TurnsThisHand++
		extra.HandPhase = AwaitingDraw
	case ActionKnock:
		r.applyKnock(state, extra, action)
	case ActionNextHand:
		// dealt in AfterAction
	}
}

func (r *Rules) applyKnock(state *game.State, extra *State, action ActionKnock) {
	knockerID := state.Players[state.CurrentTurn].ID
	opponentID := state.Players[1-state.CurrentTurn].ID

	knockerHand := state.Players[state.CurrentTurn].Cards
	opponentHand := state.Players[1-state.CurrentTurn].Cards

	result, remaining := computeKnockOutcome(knockerID, opponentID, knockerHand, opponentHand, action.Discard)

	state.Players[state.CurrentTurn].Cards = remaining
	state.Discard = deck.New([]deck.Card{})
	extra.TakenUpcard = nil
	extra.HandPhase = HandOver
	extra.HandComplete = true
	extra.LastHandResult = result

	extra.CumulativeScores[result.Winner] += result.ScoreDelta
	if handTargetReached(extra) {
		extra.MatchComplete = true
		state.OverrideNextTurn = nil
		return
	}

	nextFirst := 1 - extra.FirstActor
	extra.FirstActor = nextFirst
	state.CurrentTurn = nextFirst
	state.OverrideNextTurn = &nextFirst
}

func computeKnockOutcome(
	knockerID, opponentID string,
	knockerHand, opponentHand []deck.Card,
	discard deck.Card,
) (*HandResult, []deck.Card) {
	remaining := deck.RemoveOne(knockerHand, discard)
	knockerMelds, knockerDW, knockerPts := BestMeldSplit(remaining)
	oppMelds, oppDW, oppPts := BestMeldSplit(opponentHand)

	result := &HandResult{
		Knocker:                knockerID,
		KnockerMelds:           knockerMelds,
		KnockerDeadwood:        knockerDW,
		KnockerDeadwoodPoints:  knockerPts,
		OpponentMelds:          oppMelds,
		OpponentDeadwood:       oppDW,
		OpponentDeadwoodPoints: oppPts,
		Gin:                    knockerPts == 0,
	}

	if result.Gin {
		// Gin blocks layoffs: opponent scores raw deadwood + bonus to knocker.
		result.ScoreDelta = oppPts + GinBonus
		result.Winner = knockerID
		return result, remaining
	}

	_, remDW := ApplyLayoffs(oppDW, knockerMelds)
	remPts := sumDeadwood(remDW)
	result.LaidOffCards = laidOffDiff(oppDW, remDW)
	result.OpponentDeadwood = remDW
	result.OpponentDeadwoodPoints = remPts

	if remPts <= knockerPts {
		result.Undercut = true
		result.Winner = opponentID
		result.ScoreDelta = (knockerPts - remPts) + UndercutBonus
	} else {
		result.Winner = knockerID
		result.ScoreDelta = remPts - knockerPts
	}
	return result, remaining
}

func handTargetReached(extra *State) bool {
	return game.AnyScoreAtLeast(extra.CumulativeScores, TargetScore)
}

func (r *Rules) AfterAction(state *game.State, action game.Action) error {
	extra, ok := state.Extra.(*State)
	if !ok {
		return errors.New("invalid state type")
	}

	switch action.(type) {
	case ActionNextHand:
		return r.beginHand(state, extra)
	case ActionDrawStock, ActionDrawDiscard:
		if extra.HandPhase != AwaitingDiscard {
			return errors.New("draw failed: the pile came up empty")
		}
	case ActionDiscard:
		if handHitTheWall(state, extra) {
			extra.HandComplete = true
			extra.HandPhase = HandOver
			extra.LastHandResult = &HandResult{Wall: true}
			nextFirst := 1 - extra.FirstActor
			extra.FirstActor = nextFirst
			state.CurrentTurn = nextFirst
			state.OverrideNextTurn = &nextFirst
		}
	}
	return nil
}

// handHitTheWall reports whether the hand is out of road: the stock is down to its
// reserve, or the table has spent MaxHandTurns without anyone knocking.
func handHitTheWall(state *game.State, extra *State) bool {
	return state.Deck.Size() <= WallStockSize || extra.TurnsThisHand >= MaxHandTurns
}

func (r *Rules) CheckWinCondition(state *game.State) bool {
	extra, ok := state.Extra.(*State)
	return ok && extra.MatchComplete
}

func (r *Rules) Standings(state *game.State) []*game.Player {
	extra, ok := state.Extra.(*State)
	if !ok {
		return nil
	}
	standings := slices.Clone(state.Players)
	slices.SortStableFunc(standings, func(a, b *game.Player) int {
		return extra.CumulativeScores[b.ID] - extra.CumulativeScores[a.ID]
	})
	return standings
}

func (r *Rules) TimeoutAction(state *game.State) game.Action {
	extra, ok := state.Extra.(*State)
	if !ok {
		return nil
	}
	if extra.HandComplete {
		if extra.MatchComplete {
			return nil
		}
		return ActionNextHand{}
	}
	if state.CurrentTurn < 0 || state.CurrentTurn >= len(state.Players) {
		return nil
	}
	p := state.Players[state.CurrentTurn]
	switch extra.HandPhase {
	case AwaitingDraw:
		if state.Deck.Size() > WallStockSize {
			return ActionDrawStock{}
		}
		return ActionDrawDiscard{}
	case AwaitingDiscard:
		card, ok := autoDiscard(p.Cards, extra.TakenUpcard)
		if !ok {
			return nil
		}
		return ActionDiscard{Card: card}
	default:
		return nil
	}
}

// autoDiscard picks the move TimeoutAction plays for an absent player: the priciest
// deadwood card, skipping the one they may not discard back. It must return something
// ValidateAction accepts, or the engine re-arms and takes the seat on the next expiry.
func autoDiscard(hand []deck.Card, forbidden *deck.Card) (deck.Card, bool) {
	allowed := func(c deck.Card) bool { return forbidden == nil || c != *forbidden }

	// Split the real hand, then choose among its deadwood: splitting a pre-filtered
	// hand would optimise melds the player does not actually hold.
	_, deadwood, _ := BestMeldSplit(hand)
	if shed := slices.DeleteFunc(deadwood, func(c deck.Card) bool { return !allowed(c) }); len(shed) > 0 {
		return highestPointCard(shed), true
	}
	// Gin, or every deadwood card is the one card they may not lay back: break a meld.
	if i := slices.IndexFunc(hand, allowed); i >= 0 {
		return hand[i], true
	}
	return deck.Card{}, false
}

func (r *Rules) TurnTimeout(state *game.State) time.Duration {
	extra, ok := state.Extra.(*State)
	if !ok || !extra.HandComplete || extra.MatchComplete {
		return 0
	}
	return HandOverTimeout
}

// StandingScore is the value Standings sorted by. Only equality is read, so the
// descending order Standings applies does not need repeating here.
func (r *Rules) StandingScore(state *game.State, p *game.Player) int {
	extra, ok := state.Extra.(*State)
	if !ok {
		return 0
	}
	return extra.CumulativeScores[p.ID]
}
