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
)

// Rules implements No-Limit Texas Hold'em (one hand per match).
type Rules struct{}

// PokerRules is a compatibility alias for Rules.
type PokerRules = Rules

var (
	_ game.Rules              = (*Rules)(nil)
	_ game.PlayerLeaveHandler = (*Rules)(nil)
)

func (r *Rules) Name() string    { return "Poker" }
func (r *Rules) MinPlayers() int { return 2 }
func (r *Rules) MaxPlayers() int { return 9 }

func (r *Rules) InitialDeck() []deck.Card {
	return deck.StandardDeck()
}

func (r *Rules) InitialDealCount() int {
	return 2
}

func (r *Rules) OnGameStart(state *game.State) error {
	// Hole cards (2) already dealt; need burn+flop+turn+river (1+3+1+1 = 6) plus margin.
	if state.Deck.Size() < 8 {
		return errors.New("not enough cards to start the game")
	}

	nPlayers := len(state.Players)
	playerChips := make(map[string]uint, nPlayers)
	playerBets := make(map[string]uint, nPlayers)
	totalContributed := make(map[string]uint, nPlayers)
	acted := make(map[string]bool, nPlayers)
	allIn := make(map[string]bool, nPlayers)
	folded := make(map[string]bool, nPlayers)

	for _, p := range state.Players {
		playerChips[p.ID] = DefaultStack
	}

	smallBlind := DefaultSmallBlind
	bigBlind := DefaultBigBlind

	var sbIndex, bbIndex, dealerIndex int
	if nPlayers == 2 {
		dealerIndex = state.CurrentTurn
		sbIndex = state.CurrentTurn
		bbIndex = (state.CurrentTurn + 1) % nPlayers
	} else {
		bbIndex = (state.CurrentTurn - 1 + nPlayers) % nPlayers
		sbIndex = (state.CurrentTurn - 2 + nPlayers) % nPlayers
		dealerIndex = (state.CurrentTurn - 3 + nPlayers) % nPlayers
	}

	extra := &State{
		DealerIndex:      dealerIndex,
		SBIndex:          sbIndex,
		BBIndex:          bbIndex,
		CurrentBet:       0,
		MinRaise:         bigBlind,
		SmallBlind:       smallBlind,
		BigBlind:         bigBlind,
		Phase:            PreFlop,
		Folded:           folded,
		PlayersAllIn:     allIn,
		Table:            make([]deck.Card, 0, 5),
		PlayerChips:      playerChips,
		PlayerBets:       playerBets,
		TotalContributed: totalContributed,
		ActedThisRound:   acted,
	}

	postBlind(extra, state.Players[sbIndex], smallBlind)
	postBlind(extra, state.Players[bbIndex], bigBlind)
	extra.CurrentBet = max(extra.PlayerBets[state.Players[sbIndex].ID], extra.PlayerBets[state.Players[bbIndex].ID])

	state.Extra = extra

	first := (bbIndex + 1) % nPlayers
	if nPlayers == 2 {
		first = sbIndex
	}
	state.OverrideNextTurn = &first
	state.CurrentTurn = first
	return nil
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
	return extra.HandComplete
}

func (r *Rules) GetStandings(state *game.State) []*player.Player {
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
		state.OverrideNextTurn = nil
		return
	}

	active := activePlayers(state, extra)
	if len(active) <= 1 {
		if len(active) == 1 {
			awardUncontested(extra, active[0])
			extra.Winners = active
		}
		extra.HandComplete = true
		extra.Phase = Showdown
		state.OverrideNextTurn = nil
		return
	}

	if state.CurrentTurn >= n {
		state.CurrentTurn = 0
	}

	if bettingRoundComplete(state, extra) {
		if err := settleAndAdvance(state, extra); err != nil {
			extra.HandComplete = true
			extra.Phase = Showdown
			state.OverrideNextTurn = nil
			return
		}
		if extra.HandComplete {
			state.OverrideNextTurn = nil
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

// adjustSeatIndex maps a seat marker to its new index after the player at
// removed leaves. When the marker's own holder leaves it moves back to the
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

// --- Actions -----------------------------------------------------------------

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

func (r *Rules) PreActionCondition(state *game.State, action game.Action) error {
	extra, ok := state.Extra.(*State)
	if !ok {
		return errors.New("invalid state type")
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

func (r *Rules) PostActionCondition(state *game.State, _ game.Action) error {
	extra, ok := state.Extra.(*State)
	if !ok {
		return errors.New("invalid state type")
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
		extra.HandComplete = true
		extra.Phase = Showdown
		return nil
	}
	if len(active) == 0 {
		extra.HandComplete = true
		extra.Phase = Showdown
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
		return nil
	}
	first := firstToActPostflop(state, extra)
	state.OverrideNextTurn = &first
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
