package ginrummy

import (
	"slices"

	"github.com/Pieczasz/terminal-card/internal/deck"
)

// ApplyLayoffs extends knockerMelds with opponent deadwood cards that attach
// legally. Repeats until no card attaches (an earlier layoff can open a new end).
func ApplyLayoffs(opponentDeadwood []deck.Card, knockerMelds [][]deck.Card) (extended [][]deck.Card, remaining []deck.Card) {
	extended = cloneMelds(knockerMelds)
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

// findAttach picks the meld a card lays off onto, runs before sets. A run has two
// open ends and every attachment opens another, while a set stops dead at four:
// spending a card on the set when it also fits a run can strand the deadwood that
// would have extended the run behind it.
func findAttach(card deck.Card, melds [][]deck.Card) (int, bool) {
	for i, meld := range melds {
		if isRun(meld) && canAttach(card, meld) {
			return i, true
		}
	}
	for i, meld := range melds {
		if isSet(meld) && canAttach(card, meld) {
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
		return deck.RunOrder(a.Rank) - deck.RunOrder(b.Rank)
	})
	if card.Suit != sorted[0].Suit {
		return false
	}
	lo := deck.RunOrder(sorted[0].Rank)
	hi := deck.RunOrder(sorted[len(sorted)-1].Rank)
	v := deck.RunOrder(card.Rank)
	return v == lo-1 || v == hi+1
}

func sortMeld(meld []deck.Card) {
	if isSet(meld) {
		return
	}
	slices.SortFunc(meld, func(a, b deck.Card) int {
		return deck.RunOrder(a.Rank) - deck.RunOrder(b.Rank)
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
