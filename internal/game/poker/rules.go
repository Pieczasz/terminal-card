package poker

import (
	"errors"
	"fmt"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/player"
)

const (
	DefaultStack      uint = 1000
	DefaultSmallBlind uint = 25
	DefaultBigBlind   uint = 50
	// HandsPerMatch is how many hands a match runs for. Chips carry across them
	// and the biggest stack at the end wins, so one cold hand costs a player
	// position rather than the whole match.
	HandsPerMatch = 10
)

// Rules implements No-Limit Texas Hold'em over a HandsPerMatch-hand match.
type Rules struct{}

var (
	_ game.Rules              = (*Rules)(nil)
	_ game.PlayerLeaveHandler = (*Rules)(nil)
)

func (r *Rules) MinPlayers() int { return 2 }
func (r *Rules) MaxPlayers() int { return 9 }

func (r *Rules) InitialDeck() []deck.Card {
	return deck.StandardDeck()
}

// holeCards is what each funded seat is dealt at the start of a hand.
const holeCards = 2

// InitialDealCount is zero because a match deals a fresh hand every round, not
// once at the start: beginHand owns the deal so there is a single code path for
// it, hand one included.
func (r *Rules) InitialDealCount() int {
	return 0
}

// minDeckAfterDeal is burn+flop+turn+river plus a two-card margin, checked once
// the hole cards for the hand are out.
const minDeckAfterDeal = 1 + 3 + 1 + 1 + 2

func (r *Rules) OnGameStart(state *game.State) error {
	nPlayers := len(state.Players)
	if nPlayers == 0 {
		return errors.New("cannot start a hand with no players")
	}

	extra := &State{
		// The engine seats the first turn at random; that seat takes the button.
		DealerIndex:      state.CurrentTurn,
		SmallBlind:       DefaultSmallBlind,
		BigBlind:         DefaultBigBlind,
		HandsTotal:       HandsPerMatch,
		Folded:           make(map[string]bool, nPlayers),
		PlayersAllIn:     make(map[string]bool, nPlayers),
		Table:            make([]deck.Card, 0, 5),
		PlayerChips:      make(map[string]uint, nPlayers),
		PlayerBets:       make(map[string]uint, nPlayers),
		TotalContributed: make(map[string]uint, nPlayers),
		ActedThisRound:   make(map[string]bool, nPlayers),
	}
	for _, p := range state.Players {
		extra.PlayerChips[p.ID] = DefaultStack
	}
	state.Extra = extra

	return r.beginHand(state, extra, extra.DealerIndex)
}

// beginHand deals the next hand of the match: fresh shuffled deck, hole cards for
// everyone still holding chips, button and blinds moved on. A busted player is
// marked folded for the rest of the match so the turn cursor skips their seat.
func (r *Rules) beginHand(state *game.State, extra *State, dealer int) error {
	resetForHand(state, extra)
	extra.HandNumber++

	state.Deck = deck.New(r.InitialDeck())
	if err := state.Deck.Shuffle(); err != nil {
		return fmt.Errorf("shuffle deck: %w", err)
	}
	if err := dealHoleCards(state, extra, holeCards); err != nil {
		return err
	}
	if state.Deck.Size() < minDeckAfterDeal {
		return errors.New("not enough cards to run the board")
	}

	// Seats are counted before the blinds are posted: a blind big enough to bust a
	// short stack would otherwise make a full table look heads-up.
	headsUp := fundedSeats(state, extra) == 2
	setBlinds(state, extra, dealer, headsUp)
	postBlind(extra, state.Players[extra.SBIndex], extra.SmallBlind)
	postBlind(extra, state.Players[extra.BBIndex], extra.BigBlind)
	extra.CurrentBet = max(
		extra.PlayerBets[state.Players[extra.SBIndex].ID],
		extra.PlayerBets[state.Players[extra.BBIndex].ID],
	)

	first := firstToActPreflop(state, extra, headsUp)
	if first < 0 {
		// Every funded player was put all-in by their own blind: nobody can act,
		// so the board just runs out.
		return settleAndAdvance(state, extra)
	}
	state.CurrentTurn = first
	state.OverrideNextTurn = &first
	return nil
}

func resetForHand(state *game.State, extra *State) {
	clear(extra.Folded)
	clear(extra.PlayersAllIn)
	clear(extra.PlayerBets)
	clear(extra.TotalContributed)
	clear(extra.ActedThisRound)
	extra.Table = extra.Table[:0]
	extra.Pots = nil
	extra.Winners = nil
	extra.LastAction = nil
	extra.MainPool = 0
	extra.CurrentBet = 0
	extra.MinRaise = extra.BigBlind
	extra.Phase = PreFlop
	extra.HandComplete = false
	extra.ReachedShowdown = false
	state.Winner = nil
}

func dealHoleCards(state *game.State, extra *State, count int) error {
	funded := 0
	for _, p := range state.Players {
		if extra.PlayerChips[p.ID] == 0 {
			p.Cards = nil
			extra.Folded[p.ID] = true
			extra.ActedThisRound[p.ID] = true
			continue
		}
		cards, ok := state.Deck.DrawNCards(count)
		if !ok {
			return errors.New("insufficient number of cards to deal for all players")
		}
		p.Cards = cards
		funded++
	}
	if funded < 2 {
		return errors.New("not enough funded players to deal a hand")
	}
	return nil
}

// setBlinds puts the button on dealer and derives the blinds from it. Heads-up
// posts the small blind on the button.
func setBlinds(state *game.State, extra *State, dealer int, headsUp bool) {
	extra.DealerIndex = dealer
	if headsUp {
		extra.SBIndex = dealer
		extra.BBIndex = nextFundedSeat(state, extra, dealer)
		return
	}
	extra.SBIndex = nextFundedSeat(state, extra, dealer)
	extra.BBIndex = nextFundedSeat(state, extra, extra.SBIndex)
}

// firstToActPreflop returns the seat under the gun, skipping anyone the blinds
// already put all-in. -1 means nobody at the table can act.
func firstToActPreflop(state *game.State, extra *State, headsUp bool) int {
	// Heads-up the button acts first, so its own seat has to be considered; every
	// other table starts with the seat after the big blind.
	if headsUp && !cannotAct(extra, state.Players[extra.DealerIndex].ID) {
		return extra.DealerIndex
	}
	return nextToAct(state, extra, extra.BBIndex)
}

// recordBustouts stamps the hand each newly broke player went out on, so final
// standings can order them by how long they lasted.
func recordBustouts(state *game.State, extra *State) {
	for _, p := range state.Players {
		if extra.PlayerChips[p.ID] > 0 {
			continue
		}
		if extra.BustedAtHand == nil {
			extra.BustedAtHand = make(map[string]int, len(state.Players))
		}
		if _, done := extra.BustedAtHand[p.ID]; !done {
			extra.BustedAtHand[p.ID] = extra.HandNumber
		}
	}
}

func fundedSeats(state *game.State, extra *State) int {
	n := 0
	for _, p := range state.Players {
		if extra.PlayerChips[p.ID] > 0 {
			n++
		}
	}
	return n
}

func nextFundedSeat(state *game.State, extra *State, from int) int {
	n := len(state.Players)
	for i := 1; i <= n; i++ {
		idx := (from + i) % n
		if extra.PlayerChips[state.Players[idx].ID] > 0 {
			return idx
		}
	}
	return from
}

// finishHand closes out a hand. It ends the match once the hands run out or only
// one player still has chips; otherwise it parks the turn on the next dealer, who
// deals the following hand with ActionNextHand.
func finishHand(state *game.State, extra *State) {
	extra.HandComplete = true
	extra.Phase = Showdown
	recordBustouts(state, extra)
	if extra.HandNumber >= extra.HandsTotal || fundedSeats(state, extra) <= 1 {
		extra.MatchComplete = true
		state.OverrideNextTurn = nil
		return
	}
	next := nextFundedSeat(state, extra, extra.DealerIndex)
	state.CurrentTurn = next
	state.OverrideNextTurn = &next
}

func postBlind(extra *State, p *player.Player, amount uint) {
	pay := min(amount, extra.PlayerChips[p.ID])
	extra.PlayerChips[p.ID] -= pay
	extra.PlayerBets[p.ID] += pay
	extra.TotalContributed[p.ID] += pay
	extra.MainPool += pay
	if extra.PlayerChips[p.ID] == 0 {
		extra.PlayersAllIn[p.ID] = true
	}
}

func (r *Rules) CheckWinCondition(state *game.State) bool {
	extra, ok := state.Extra.(*State)
	if !ok {
		return false
	}
	return extra.MatchComplete
}

func (r *Rules) Standings(state *game.State) []*player.Player {
	extra, ok := state.Extra.(*State)
	if !ok {
		return nil
	}
	return rankPlayers(state, extra)
}

// OnPlayerLeave folds the departing player. Turn and seat resolution runs in
// AfterPlayerRemoved, once the seats have actually shifted.
func (r *Rules) OnPlayerLeave(state *game.State, playerID string) {
	extra, ok := state.Extra.(*State)
	if !ok || extra.HandComplete {
		return
	}
	extra.Folded[playerID] = true
	extra.ActedThisRound[playerID] = true
}

// AfterPlayerRemoved reindexes the button/blinds and picks the next actor from
// post-removal seats, so a fold-on-disconnect never leaves a stale pre-removal
// index on turn.
func (r *Rules) AfterPlayerRemoved(state *game.State, removedIndex int) {
	extra, ok := state.Extra.(*State)
	if !ok {
		return
	}
	n := len(state.Players)
	if n == 0 {
		return
	}
	extra.DealerIndex = adjustSeatIndex(extra.DealerIndex, removedIndex, n)
	extra.SBIndex = adjustSeatIndex(extra.SBIndex, removedIndex, n)
	extra.BBIndex = adjustSeatIndex(extra.BBIndex, removedIndex, n)

	if extra.HandComplete {
		// The player who was due to deal may be the one who just left, so the turn
		// is re-parked rather than left pointing at an empty seat.
		finishHand(state, extra)
		return
	}

	active := activePlayers(state, extra)
	if len(active) <= 1 {
		if len(active) == 1 {
			awardUncontested(extra, active[0])
			extra.Winners = active
		}
		finishHand(state, extra)
		return
	}

	if state.CurrentTurn >= n {
		state.CurrentTurn = 0
	}

	if bettingRoundComplete(state, extra) {
		if err := settleAndAdvance(state, extra); err != nil {
			finishHand(state, extra)
			return
		}
		if extra.HandComplete {
			finishHand(state, extra)
			return
		}
		first := firstToActPostflop(state, extra)
		state.CurrentTurn = first
		state.OverrideNextTurn = &first
		return
	}

	idx := state.CurrentTurn
	if cannotAct(extra, state.Players[idx].ID) {
		if next := nextToAct(state, extra, idx); next >= 0 {
			idx = next
		}
	}
	state.CurrentTurn = idx
	state.OverrideNextTurn = &idx
}

func cannotAct(extra *State, id string) bool {
	return isFolded(extra, id) || extra.PlayersAllIn[id] || extra.PlayerChips[id] == 0
}

// adjustSeatIndex maps a seat marker to its new index after the player
// removed leaves. When the marker's own holder leaves, it moves back to the
// previous seat rather than silently landing on whoever shifted into the slot.
func adjustSeatIndex(seat, removed, nAfter int) int {
	if nAfter <= 0 {
		return 0
	}
	switch {
	case seat > removed:
		seat--
	case seat == removed:
		seat = (removed - 1 + nAfter) % nAfter
	}
	if seat >= nAfter {
		seat = nAfter - 1
	}
	if seat < 0 {
		seat = 0
	}
	return seat
}

type ActionFold struct{}

func (a ActionFold) Name() string { return "poker.Fold" }

type ActionCheck struct{}

func (a ActionCheck) Name() string { return "poker.Check" }

type ActionCall struct{}

func (a ActionCall) Name() string { return "poker.Call" }

type ActionRaiseTo struct {
	Amount uint
}

func (a ActionRaiseTo) Name() string { return "poker.RaiseTo" }

type ActionAllIn struct{}

func (a ActionAllIn) Name() string { return "poker.AllIn" }

// ActionNextHand deals the next hand of the match. Only the incoming dealer, who
// holds the turn while the result screen is up, may submit it.
type ActionNextHand struct{}

func (a ActionNextHand) Name() string { return "poker.NextHand" }

func (r *Rules) ValidateAction(state *game.State, action game.Action) error {
	extra, ok := state.Extra.(*State)
	if !ok {
		return errors.New("invalid state type")
	}
	if _, isNextHand := action.(ActionNextHand); isNextHand {
		if !extra.HandComplete {
			return errors.New("the hand is still being played")
		}
		if extra.MatchComplete {
			return errors.New("the match is over")
		}
		return nil
	}
	if extra.HandComplete || extra.Phase == Showdown {
		return errors.New("hand is over")
	}

	p := state.Players[state.CurrentTurn]
	if isFolded(extra, p.ID) || extra.PlayersAllIn[p.ID] {
		return errors.New("player cannot act")
	}

	toCall := ToCall(extra, p.ID)

	switch action := action.(type) {
	case ActionFold:
		return nil
	case ActionCheck:
		if toCall > 0 {
			return errors.New("cannot check, must call or raise")
		}
		return nil
	case ActionCall:
		if toCall == 0 {
			return errors.New("nothing to call")
		}
		return nil
	case ActionRaiseTo:
		return validateRaiseTo(extra, p, action.Amount)
	case ActionAllIn:
		if extra.PlayerChips[p.ID] == 0 {
			return errors.New("no chips to go all-in")
		}
		return nil
	default:
		return errors.New("action not allowed in poker")
	}
}

func validateRaiseTo(extra *State, p *player.Player, amount uint) error {
	if amount <= extra.CurrentBet {
		return errors.New("raise must be above current bet")
	}
	additional := amount - extra.PlayerBets[p.ID]
	if additional > extra.PlayerChips[p.ID] {
		return errors.New("not enough chips")
	}
	raiseBy := amount - extra.CurrentBet
	if additional < extra.PlayerChips[p.ID] && raiseBy < extra.MinRaise {
		return fmt.Errorf("minimum raise is %d", extra.MinRaise)
	}
	return nil
}

func (r *Rules) ApplyAction(state *game.State, action game.Action) {
	extra, ok := state.Extra.(*State)
	if !ok {
		return
	}
	if _, isNextHand := action.(ActionNextHand); isNextHand {
		// Dealing happens in AfterAction, the only hook that can report a bad deal.
		return
	}
	p := state.Players[state.CurrentTurn]

	switch action := action.(type) {
	case ActionFold:
		extra.Folded[p.ID] = true
		extra.ActedThisRound[p.ID] = true
	case ActionCheck:
		extra.ActedThisRound[p.ID] = true
	case ActionCall:
		callTo(extra, p, extra.CurrentBet)
		extra.ActedThisRound[p.ID] = true
	case ActionRaiseTo:
		commitTo(extra, p, action.Amount)
		applyBetIncrease(extra, state, p, extra.PlayerBets[p.ID])
		extra.ActedThisRound[p.ID] = true
	case ActionAllIn:
		newBet := extra.PlayerBets[p.ID] + extra.PlayerChips[p.ID]
		wasRaise := newBet > extra.CurrentBet
		commitTo(extra, p, newBet)
		if wasRaise {
			applyBetIncrease(extra, state, p, extra.PlayerBets[p.ID])
		}
		extra.ActedThisRound[p.ID] = true
	}
	extra.LastAction = action
}

func callTo(extra *State, p *player.Player, target uint) {
	if target < extra.PlayerBets[p.ID] {
		return
	}
	needed := min(target-extra.PlayerBets[p.ID], extra.PlayerChips[p.ID])
	commitTo(extra, p, extra.PlayerBets[p.ID]+needed)
}

func commitTo(extra *State, p *player.Player, streetTotal uint) {
	if streetTotal < extra.PlayerBets[p.ID] {
		return
	}
	additional := streetTotal - extra.PlayerBets[p.ID]
	if additional > extra.PlayerChips[p.ID] {
		additional = extra.PlayerChips[p.ID]
		streetTotal = extra.PlayerBets[p.ID] + additional
	}
	extra.PlayerChips[p.ID] -= additional
	extra.PlayerBets[p.ID] = streetTotal
	extra.TotalContributed[p.ID] += additional
	extra.MainPool += additional
	if extra.PlayerChips[p.ID] == 0 {
		extra.PlayersAllIn[p.ID] = true
	}
}

// applyBetIncrease raises CurrentBet to newBet. Only a full-size raise
// (>= MinRaise) reopens the round; a sub-minimum all-in advances the amount
// owed without granting already-acted players fresh action.
func applyBetIncrease(extra *State, state *game.State, raiser *player.Player, newBet uint) {
	if newBet <= extra.CurrentBet {
		return
	}
	raiseSize := newBet - extra.CurrentBet
	full := raiseSize >= extra.MinRaise
	extra.CurrentBet = newBet
	if full {
		extra.MinRaise = raiseSize
		resetActedExcept(extra, state, raiser.ID)
	}
}

func resetActedExcept(extra *State, state *game.State, exceptID string) {
	for _, p := range state.Players {
		if p.ID == exceptID || isFolded(extra, p.ID) || extra.PlayersAllIn[p.ID] {
			continue
		}
		extra.ActedThisRound[p.ID] = false
	}
}

func (r *Rules) AfterAction(state *game.State, action game.Action) error {
	extra, ok := state.Extra.(*State)
	if !ok {
		return errors.New("invalid state type")
	}
	if _, isNextHand := action.(ActionNextHand); isNextHand {
		if err := r.beginHand(state, extra, nextFundedSeat(state, extra, extra.DealerIndex)); err != nil {
			return err
		}
		if extra.HandComplete {
			finishHand(state, extra)
		}
		return nil
	}
	return r.afterBettingAction(state, extra)
}

func (r *Rules) afterBettingAction(state *game.State, extra *State) error {
	if extra.HandComplete {
		return nil
	}

	active := activePlayers(state, extra)
	if len(active) == 1 {
		awardUncontested(extra, active[0])
		extra.Winners = []*player.Player{active[0]}
		finishHand(state, extra)
		return nil
	}
	if len(active) == 0 {
		finishHand(state, extra)
		return nil
	}

	if !bettingRoundComplete(state, extra) {
		next := nextToAct(state, extra, state.CurrentTurn)
		if next >= 0 {
			state.OverrideNextTurn = &next
			return nil
		}
	}

	if err := settleAndAdvance(state, extra); err != nil {
		return err
	}
	if extra.HandComplete {
		finishHand(state, extra)
		return nil
	}
	state.OverrideNextTurn = new(firstToActPostflop(state, extra))
	return nil
}

// ToCall returns chips the player must add to match CurrentBet.
func ToCall(extra *State, playerID string) uint {
	if extra.CurrentBet <= extra.PlayerBets[playerID] {
		return 0
	}
	return extra.CurrentBet - extra.PlayerBets[playerID]
}

func isFolded(extra *State, playerID string) bool {
	return extra.Folded[playerID]
}

func activePlayers(state *game.State, extra *State) []*player.Player {
	out := make([]*player.Player, 0, len(state.Players))
	for _, p := range state.Players {
		if !isFolded(extra, p.ID) {
			out = append(out, p)
		}
	}
	return out
}
