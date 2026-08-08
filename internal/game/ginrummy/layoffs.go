package ginrummy

import (
	"slices"

	"github.com/Pieczasz/terminal-card/internal/deck"
)

// ApplyLayoffs extends knockerMelds with opponent deadwood cards that attach
// legally. Repeats until no card attaches (an earlier layoff can open a new end).
func ApplyLayoffs(opponentDeadwood []deck.Card, knockerMelds [][]deck.Card) (extended [][]deck.Card, remaining []deck.Card) {
	extended = make([][]deck.Card, len(knockerMelds))
	for i, m := range knockerMelds {
		extended[i] = slices.Clone(m)
	}
	remaining = slices.Clone(opponentDeadwood)

	for changed := true; changed; {
		changed = false
		for i := 0; i < len(remaining); {
			if idx, ok := findAttach(remaining[i], extended); ok {
				extended[idx] = append(extended[idx], remaining[i])
				sortMeld(extended[idx])
				remaining = append(remaining[:i], remaining[i+1:]...)
				changed = true
				continue
			}
			i++
		}
	}
	return extended, remaining
}

func findAttach(card deck.Card, melds [][]deck.Card) (int, bool) {
	for i, meld := range melds {
		if canAttach(card, meld) {
			return i, true
		}
	}
	return 0, false
}

func canAttach(card deck.Card, meld []deck.Card) bool {
	if isSet(meld) {
		return len(meld) < 4 && card.Rank == meld[0].Rank
	}
	if !isRun(meld) {
		return false
	}
	sorted := slices.Clone(meld)
	slices.SortFunc(sorted, func(a, b deck.Card) int {
		return rankOrder(a.Rank) - rankOrder(b.Rank)
	})
	if card.Suit != sorted[0].Suit {
		return false
	}
	lo := rankOrder(sorted[0].Rank)
	hi := rankOrder(sorted[len(sorted)-1].Rank)
	v := rankOrder(card.Rank)
	return v == lo-1 || v == hi+1
}

func sortMeld(meld []deck.Card) {
	if isSet(meld) {
		return
	}
	slices.SortFunc(meld, func(a, b deck.Card) int {
		return rankOrder(a.Rank) - rankOrder(b.Rank)
	})
}

func laidOffDiff(before, after []deck.Card) []deck.Card {
	remaining := make(map[deck.Card]int, len(after))
	for _, c := range after {
		remaining[c]++
	}
	var laid []deck.Card
	for _, c := range before {
		if remaining[c] > 0 {
			remaining[c]--
			continue
		}
		laid = append(laid, c)
	}
	return laid
}
