package poker

import (
	"cmp"
	"errors"
	"log/slog"
	"maps"
	"slices"

	"github.com/Pieczasz/terminal-card/internal/game"
)

func bettingRoundComplete(state *game.State, extra *State) bool {
	for _, p := range state.Players {
		if cannotAct(extra, p.ID) {
			continue
		}
		if !extra.ActedThisRound[p.ID] {
			return false
		}
		if extra.PlayerBets[p.ID] < extra.CurrentBet {
			return false
		}
	}
	return true
}

// nextSeat scans clockwise from seat from, wrapping once around a table of n seats,
// and returns the first index ok accepts. -1 means no seat qualifies.
func nextSeat(from, n int, ok func(idx int) bool) int {
	for i := 1; i <= n; i++ {
		idx := (from + i) % n
		if ok(idx) {
			return idx
		}
	}
	return -1
}

func nextToAct(state *game.State, extra *State, from int) int {
	return nextSeat(from, len(state.Players), func(idx int) bool {
		id := state.Players[idx].ID
		if cannotAct(extra, id) {
			return false
		}
		return !extra.ActedThisRound[id] || extra.PlayerBets[id] < extra.CurrentBet
	})
}

func firstToActPostflop(state *game.State, extra *State) int {
	idx := nextSeat(extra.DealerIndex, len(state.Players), func(idx int) bool {
		return !cannotAct(extra, state.Players[idx].ID)
	})
	if idx < 0 {
		return state.CurrentTurn
	}
	return idx
}

func settleAndAdvance(state *game.State, extra *State) error {
	for _, p := range state.Players {
		extra.PlayerBets[p.ID] = 0
		extra.ActedThisRound[p.ID] = false
	}
	extra.CurrentBet = 0
	extra.MinRaise = extra.BigBlind

	// Betting can only continue with at least two players who still have chips;
	// a lone live player against all-ins just runs the board out.
	canStillBet := 0
	for _, p := range activePlayers(state, extra) {
		if !extra.PlayersAllIn[p.ID] && extra.PlayerChips[p.ID] > 0 {
			canStillBet++
		}
	}

	dealt, err := advanceStreet(state, extra)
	if err != nil {
		return err
	}
	if !dealt {
		return runShowdown(state, extra)
	}
	if canStillBet < 2 {
		return runOutBoard(state, extra)
	}
	return nil
}

// advanceStreet burns, deals what the next street needs and moves Phase onto it. It
// reports false once there is no street left to deal, which is the showdown.
func advanceStreet(state *game.State, extra *State) (bool, error) {
	var next RoundPhase
	var cards int
	switch extra.Phase {
	case PreFlop:
		next, cards = Flop, 3
	case Flop:
		next, cards = Turn, 1
	case Turn:
		next, cards = River, 1
	default:
		return false, nil
	}
	if err := dealCommunity(state, extra, cards); err != nil {
		return false, err
	}
	extra.Phase = next
	return true, nil
}

func dealCommunity(state *game.State, extra *State, n int) error {
	if _, ok := state.Deck.Draw(); !ok {
		slog.Error("poker deck empty during burn", "phase", extra.Phase.String(), "hand", extra.HandNumber)
		return errors.New("deck empty during burn")
	}
	for range n {
		c, ok := state.Deck.Draw()
		if !ok {
			slog.Error("poker deck empty during community deal",
				"phase", extra.Phase.String(), "hand", extra.HandNumber, "want", n)
			return errors.New("deck empty during community deal")
		}
		extra.Table = append(extra.Table, c)
	}
	return nil
}

func runOutBoard(state *game.State, extra *State) error {
	for extra.Phase != River && extra.Phase != Showdown {
		dealt, err := advanceStreet(state, extra)
		if err != nil {
			return err
		}
		if !dealt {
			break
		}
	}
	return runShowdown(state, extra)
}

func runShowdown(state *game.State, extra *State) error {
	extra.Phase = Showdown
	extra.ReachedShowdown = true
	live := contenders(state, extra)
	scores := handScores(live, extra)
	extra.Pots = buildSidePots(extra, live)
	extra.HandComplete = true
	extra.Winners = awardPots(extra, live, scores)
	return nil
}

// contenders is everyone still contesting the pot: the seated players who have not
// folded, plus anyone who left the table while all-in. An all-in player has no
// decisions left to make, so disconnecting cannot cost them a pot they are already
// committed to - leaving still forfeits the hand for anyone with chips behind.
func contenders(state *game.State, extra *State) []*game.Player {
	out := activePlayers(state, extra)
	for _, p := range state.LeftPlayers {
		if !isFolded(extra, p.ID) && extra.PlayersAllIn[p.ID] {
			out = append(out, p)
		}
	}
	return out
}

func buildSidePots(extra *State, live []*game.Player) []Pot {
	eligibleIDs := make(map[string]bool, len(live))
	for _, p := range live {
		eligibleIDs[p.ID] = true
	}

	// Distinct non-zero contribution levels, ascending: each one closes a pot
	// layer. Contributions come from every player who put chips in, seated or not.
	levels := slices.Sorted(maps.Values(extra.TotalContributed))
	levels = slices.Compact(levels)
	levels = slices.DeleteFunc(levels, func(c uint) bool { return c == 0 })

	var pots []Pot
	var orphan uint
	prev := uint(0)
	for _, lvl := range levels {
		var eligible []string
		var amount uint
		for id, contrib := range extra.TotalContributed {
			// A contribution below this level is always below prev too: every
			// non-zero contribution is itself one of the levels, so by the time the
			// loop passes it, prev has already reached it. Nothing to collect.
			if contrib < lvl {
				continue
			}
			amount += lvl - prev
			if eligibleIDs[id] {
				eligible = append(eligible, id)
			}
		}
		prev = lvl
		amount += orphan
		orphan = 0
		if amount == 0 {
			continue
		}
		if len(eligible) == 0 {
			// Dead money carries forward to the next pot layer.
			orphan = amount
			continue
		}
		slices.Sort(eligible) // stable pot eligibility order
		pots = append(pots, Pot{Amount: amount, Eligible: eligible})
	}
	if orphan > 0 {
		distributeOrphan(extra, pots, live, orphan)
	}
	return pots
}

// distributeOrphan places chips no pot layer could claim. They ride along with the
// last layer that formed; if none did, there is no pot left for anyone to win them
// from, so they go straight into the stacks of whoever is still in the hand rather
// than back through MainPool.
func distributeOrphan(extra *State, pots []Pot, live []*game.Player, orphan uint) {
	if len(pots) > 0 {
		pots[len(pots)-1].Amount += orphan
		return
	}
	if len(live) == 0 {
		return
	}
	ids := make([]string, 0, len(live))
	for _, p := range live {
		ids = append(ids, p.ID)
	}
	slices.Sort(ids)
	splitEvenly(extra, ids, orphan)
}

// splitEvenly hands amount to ids, the odd chips going one each to the front of the
// slice. Callers sort ids first, which is a deviation worth naming: a casino gives the
// odd chip to the first player left of the button, this gives it to the lowest-sorted
// player ID. It is deterministic, which is what matters for a replayable table.
func splitEvenly(extra *State, ids []string, amount uint) {
	share := amount / uint(len(ids))
	rem := amount % uint(len(ids))
	for i, id := range ids {
		extra.PlayerChips[id] += share
		if uint(i) < rem {
			extra.PlayerChips[id]++
		}
	}
}

// awardPots pays every pot to the best hand among that pot's own eligible players and
// returns everyone who took a share, main pot first.
//
// Eligibility is what makes this the authoritative winner list: the best hand at the
// table can belong to a short stack who only paid into the main pot, so a global
// best-hand scan would announce a winner the side pot did not go to.
func awardPots(extra *State, live []*game.Player, scores map[string]int) []*game.Player {
	playerByID := make(map[string]*game.Player, len(live))
	for _, p := range live {
		playerByID[p.ID] = p
	}

	var winners []*game.Player
	for _, pot := range extra.Pots {
		if pot.Amount == 0 || len(pot.Eligible) == 0 {
			continue
		}
		bestScore := -1
		var potWinners []string
		for _, id := range pot.Eligible {
			if playerByID[id] == nil {
				continue
			}
			switch score := scores[id]; {
			case score > bestScore:
				bestScore = score
				potWinners = append(potWinners[:0], id)
			case score == bestScore:
				potWinners = append(potWinners, id)
			}
		}
		if len(potWinners) == 0 {
			continue
		}
		slices.Sort(potWinners)
		splitEvenly(extra, potWinners, pot.Amount)
		for _, id := range potWinners {
			if p := playerByID[id]; !slices.Contains(winners, p) {
				winners = append(winners, p)
			}
		}
	}
	extra.MainPool = 0
	return winners
}

func handScore(p *game.Player, extra *State) int {
	cards := slices.Clone(p.Cards)
	cards = append(cards, extra.Table...)
	return evaluateHand(cards)
}

// handScores evaluates each player's hand once, so callers avoid re-running the
// allocating evaluator inside a sort comparator or per-pot loop.
func handScores(players []*game.Player, extra *State) map[string]int {
	scores := make(map[string]int, len(players))
	for _, p := range players {
		scores[p.ID] = handScore(p, extra)
	}
	return scores
}

func awardUncontested(extra *State, winner *game.Player) {
	extra.PlayerChips[winner.ID] += extra.MainPool
	extra.MainPool = 0
	extra.Pots = nil
}

// chipsInPlay is the invariant every betting path must preserve: chips only ever move
// between a player's stack and the pool, so the two together are constant for the
// whole hand.
func chipsInPlay(extra *State) uint {
	total := extra.MainPool
	for _, c := range extra.PlayerChips {
		total += c
	}
	return total
}

// rankPlayers ranks everyone who sat down, with the players who walked out last.
// Leaving mid-match forfeits the match, so no leaver places above someone who saw
// it through - but leavers are still ranked against each other on what they won
// while they were playing, not on who happened to quit first.
func rankPlayers(state *game.State, extra *State) []*game.Player {
	byResult := resultOrder(state, extra)

	seated := slices.Clone(state.Players)
	left := slices.Clone(state.LeftPlayers)
	slices.SortFunc(seated, byResult)
	slices.SortFunc(left, byResult)
	return slices.Concat(seated, left)
}

// resultOrder compares two players by: chips desc, bust-out hand desc, active
// before folded, hand score desc, ID asc. Chips lead because a match is decided by
// the stack a player walks away with; everyone who busted is level on chips, so how
// long they lasted is what separates them. The hand-level keys only matter for
// players who finished holding equal stacks.
func resultOrder(state *game.State, extra *State) func(a, b *game.Player) int {
	scores := handScores(slices.Concat(state.Players, state.LeftPlayers), extra)
	return func(a, b *game.Player) int {
		if c := cmp.Compare(extra.PlayerChips[b.ID], extra.PlayerChips[a.ID]); c != 0 {
			return c
		}
		if c := cmp.Compare(extra.BustedAtHand[b.ID], extra.BustedAtHand[a.ID]); c != 0 {
			return c
		}
		fa, fb := isFolded(extra, a.ID), isFolded(extra, b.ID)
		if fa != fb {
			if fa {
				return 1
			}
			return -1
		}
		if !fa && len(extra.Table) >= 3 {
			if c := cmp.Compare(scores[b.ID], scores[a.ID]); c != 0 {
				return c
			}
		}
		return cmp.Compare(a.ID, b.ID)
	}
}
