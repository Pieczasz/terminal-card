package hearts

import (
	"slices"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/player"
)

// ponytail: duplicated from poker; promote to deck.RankValue when a third trick-taker lands.
func rankValue(r deck.Rank) int {
	if r == deck.Ace {
		return 14
	}
	return int(r) + 1
}

func findTwoOfClubs(state *game.State) int {
	for i, p := range state.Players {
		if slices.Contains(p.Cards, twoOfClubs) {
			return i
		}
	}
	return 0
}

func passRecipient(from int, dir PassDirection, n int) int {
	switch dir {
	case PassLeft:
		return (from + 1) % n
	case PassRight:
		return (from - 1 + n) % n
	case PassAcross:
		return (from + 2) % n
	default:
		return from
	}
}

func applyAllPasses(state *game.State, extra *State) {
	n := len(state.Players)
	for i, p := range state.Players {
		recipient := passRecipient(i, extra.PassDirection, n)
		state.Players[recipient].Cards = append(state.Players[recipient].Cards, extra.PendingPasses[p.ID]...)
	}
}

func nextUnpassedSeat(state *game.State, extra *State, from int) int {
	n := len(state.Players)
	for step := 1; step <= n; step++ {
		seat := (from + step) % n
		if !extra.Passed[state.Players[seat].ID] {
			return seat
		}
	}
	return from
}

func removeCard(hand []deck.Card, card deck.Card) []deck.Card {
	out := make([]deck.Card, 0, len(hand)-1)
	removed := false
	for _, c := range hand {
		if c == card && !removed {
			removed = true
			continue
		}
		out = append(out, c)
	}
	return out
}

func removeCards(hand []deck.Card, cards []deck.Card) []deck.Card {
	out := hand
	for _, c := range cards {
		out = removeCard(out, c)
	}
	return out
}

func handHasSuit(hand []deck.Card, suit deck.Suit) bool {
	for _, c := range hand {
		if c.Suit == suit {
			return true
		}
	}
	return false
}

func onlyHearts(hand []deck.Card) bool {
	for _, c := range hand {
		if c.Suit != deck.Hearts {
			return false
		}
	}
	return len(hand) > 0
}

func isPenaltyCard(c deck.Card) bool {
	return c.Suit == deck.Hearts || c == queenOfSpades
}

func hasNonPenaltyCard(hand []deck.Card) bool {
	for _, c := range hand {
		if !isPenaltyCard(c) {
			return true
		}
	}
	return false
}

func trickWinner(state *game.State, extra *State) (string, int) {
	bestValue := -1
	winnerSeat := extra.TrickLeader
	for seat, p := range state.Players {
		card, ok := extra.TrickCards[p.ID]
		if !ok || card.Suit != extra.LedSuit {
			continue
		}
		if v := rankValue(card.Rank); v > bestValue {
			bestValue = v
			winnerSeat = seat
		}
	}
	return state.Players[winnerSeat].ID, winnerSeat
}

func trickPoints(cards map[string]deck.Card) int {
	pts := 0
	for _, c := range cards {
		if c.Suit == deck.Hearts {
			pts++
		}
		if c == queenOfSpades {
			pts += 13
		}
	}
	return pts
}

func scoreHand(extra *State, players []*player.Player) {
	shooterID := ""
	for _, p := range players {
		if extra.HandPoints[p.ID] == penaltyPointsTotal {
			shooterID = p.ID
			break
		}
	}
	if shooterID != "" {
		for _, p := range players {
			if p.ID != shooterID {
				extra.CumulativeScores[p.ID] += penaltyPointsTotal
			}
		}
		return
	}
	for _, p := range players {
		extra.CumulativeScores[p.ID] += extra.HandPoints[p.ID]
	}
}

func handTargetReached(extra *State) bool {
	for _, s := range extra.CumulativeScores {
		if s >= extra.TargetScore {
			return true
		}
	}
	return false
}

func threeLowestCards(hand []deck.Card) []deck.Card {
	if len(hand) <= cardsToPass {
		return slices.Clone(hand)
	}
	sorted := slices.Clone(hand)
	slices.SortFunc(sorted, func(a, b deck.Card) int {
		if d := rankValue(a.Rank) - rankValue(b.Rank); d != 0 {
			return d
		}
		return int(a.Suit) - int(b.Suit)
	})
	return sorted[:cardsToPass]
}

func firstLegalCard(state *game.State, extra *State, p *player.Player) (deck.Card, bool) {
	for _, c := range p.Cards {
		if validatePlayCard(state, extra, p, c) == nil {
			return c, true
		}
	}
	return deck.Card{}, false
}
