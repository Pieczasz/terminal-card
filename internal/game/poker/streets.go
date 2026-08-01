package poker

import (
	"errors"
	"maps"
	"slices"
	"strings"

	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/player"
)

func bettingRoundComplete(state *game.State, extra *State) bool {
	for _, p := range state.Players {
		if isFolded(extra, p.ID) || extra.PlayersAllIn[p.ID] || extra.PlayerChips[p.ID] == 0 {
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

func nextToAct(state *game.State, extra *State, from int) int {
	n := len(state.Players)
	for i := 1; i <= n; i++ {
		idx := (from + i) % n
		p := state.Players[idx]
		if isFolded(extra, p.ID) || extra.PlayersAllIn[p.ID] || extra.PlayerChips[p.ID] == 0 {
			continue
		}
		if !extra.ActedThisRound[p.ID] || extra.PlayerBets[p.ID] < extra.CurrentBet {
			return idx
		}
	}
	return -1
}

func firstToActPostflop(state *game.State, extra *State) int {
	n := len(state.Players)
	for i := 1; i <= n; i++ {
		idx := (extra.DealerIndex + i) % n
		p := state.Players[idx]
		if !isFolded(extra, p.ID) && !extra.PlayersAllIn[p.ID] && extra.PlayerChips[p.ID] > 0 {
			return idx
		}
	}
	return state.CurrentTurn
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

	switch extra.Phase {
	case PreFlop:
		if err := dealCommunity(state, extra, 3); err != nil {
			return err
		}
		extra.Phase = Flop
	case Flop:
		if err := dealCommunity(state, extra, 1); err != nil {
			return err
		}
		extra.Phase = Turn
	case Turn:
		if err := dealCommunity(state, extra, 1); err != nil {
			return err
		}
		extra.Phase = River
	case River:
		return runShowdown(state, extra)
	default:
		return runShowdown(state, extra)
	}

	if canStillBet < 2 {
		return runOutBoard(state, extra)
	}
	return nil
}

func dealCommunity(state *game.State, extra *State, n int) error {
	if _, ok := state.Deck.Draw(); !ok {
		return errors.New("deck empty during burn")
	}
	for range n {
		c, ok := state.Deck.Draw()
		if !ok {
			return errors.New("deck empty during community deal")
		}
		extra.Table = append(extra.Table, c)
	}
	return nil
}

func runOutBoard(state *game.State, extra *State) error {
	for extra.Phase != River && extra.Phase != Showdown {
		switch extra.Phase {
		case Flop:
			if err := dealCommunity(state, extra, 1); err != nil {
				return err
			}
			extra.Phase = Turn
		case Turn:
			if err := dealCommunity(state, extra, 1); err != nil {
				return err
			}
			extra.Phase = River
		case PreFlop:
			if err := dealCommunity(state, extra, 3); err != nil {
				return err
			}
			extra.Phase = Flop
		default:
			return runShowdown(state, extra)
		}
	}
	return runShowdown(state, extra)
}

func runShowdown(state *game.State, extra *State) error {
	extra.Phase = Showdown
	scores := handScores(state.Players, extra)
	extra.Pots = buildSidePots(state, extra)
	awardPots(state, extra, scores)
	extra.HandComplete = true
	extra.Winners = showdownWinners(state, extra, scores)
	return nil
}

// showdownWinners returns every non-folded player tying for the best hand, so a
// split pot names all co-winners rather than only the first.
func showdownWinners(state *game.State, extra *State, scores map[string]int) []*player.Player {
	best := -1
	for _, p := range state.Players {
		if !isFolded(extra, p.ID) && scores[p.ID] > best {
			best = scores[p.ID]
		}
	}
	var winners []*player.Player
	for _, p := range state.Players {
		if !isFolded(extra, p.ID) && scores[p.ID] == best {
			winners = append(winners, p)
		}
	}
	return winners
}

func buildSidePots(state *game.State, extra *State) []Pot {
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
			if contrib >= lvl {
				amount += lvl - prev
				if !isFolded(extra, id) && playerStillSeated(state, id) {
					eligible = append(eligible, id)
				}
			} else if contrib > prev {
				amount += contrib - prev
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
		if len(pots) > 0 {
			pots[len(pots)-1].Amount += orphan
		} else {
			// Last resort: award orphan to any remaining active player via MainPool path.
			active := activePlayers(state, extra)
			if len(active) == 1 {
				extra.PlayerChips[active[0].ID] += orphan
			} else if len(active) > 0 {
				// Split evenly among active if no pot layers formed (should be rare).
				share := orphan / uint(len(active))
				rem := orphan % uint(len(active))
				for i, p := range active {
					extra.PlayerChips[p.ID] += share
					if uint(i) < rem {
						extra.PlayerChips[p.ID]++
					}
				}
			}
		}
	}
	return pots
}

func playerStillSeated(state *game.State, id string) bool {
	for _, p := range state.Players {
		if p.ID == id {
			return true
		}
	}
	return false
}

func awardPots(state *game.State, extra *State, scores map[string]int) {
	playerByID := map[string]*player.Player{}
	for _, p := range state.Players {
		playerByID[p.ID] = p
	}

	for _, pot := range extra.Pots {
		if pot.Amount == 0 || len(pot.Eligible) == 0 {
			continue
		}
		bestScore := -1
		var winners []string
		for _, id := range pot.Eligible {
			if playerByID[id] == nil {
				continue
			}
			score := scores[id]
			if score > bestScore {
				bestScore = score
				winners = []string{id}
			} else if score == bestScore {
				winners = append(winners, id)
			}
		}
		if len(winners) == 0 {
			continue
		}
		slices.Sort(winners) // stable odd-chip remainder assignment
		share := pot.Amount / uint(len(winners))
		rem := pot.Amount % uint(len(winners))
		for i, id := range winners {
			extra.PlayerChips[id] += share
			if uint(i) < rem {
				extra.PlayerChips[id]++
			}
		}
	}
	extra.MainPool = 0
}

func handScore(p *player.Player, extra *State) int {
	cards := slices.Clone(p.Cards)
	cards = append(cards, extra.Table...)
	return EvaluateHand(cards)
}

// handScores evaluates each player's hand once, so callers avoid re-running the
// allocating evaluator inside a sort comparator or per-pot loop.
func handScores(players []*player.Player, extra *State) map[string]int {
	scores := make(map[string]int, len(players))
	for _, p := range players {
		scores[p.ID] = handScore(p, extra)
	}
	return scores
}

func awardUncontested(extra *State, winner *player.Player) {
	extra.PlayerChips[winner.ID] += extra.MainPool
	extra.MainPool = 0
	extra.Pots = nil
}

// rankPlayers orders by: active before folded, hand score desc, chips desc, ID asc.
func rankPlayers(state *game.State, extra *State) []*player.Player {
	players := slices.Clone(state.Players)
	scores := handScores(state.Players, extra)
	slices.SortFunc(players, func(a, b *player.Player) int {
		fa, fb := isFolded(extra, a.ID), isFolded(extra, b.ID)
		if fa != fb {
			if fa {
				return 1
			}
			return -1
		}
		if !fa && len(extra.Table) >= 3 {
			sa, sb := scores[a.ID], scores[b.ID]
			if sa != sb {
				return sb - sa
			}
		}
		ca, cb := extra.PlayerChips[a.ID], extra.PlayerChips[b.ID]
		if ca != cb {
			if ca > cb {
				return -1
			}
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	return players
}
