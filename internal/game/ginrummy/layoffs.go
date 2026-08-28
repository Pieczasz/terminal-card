package ginrummy

import (
	"slices"

	"github.com/Pieczasz/terminal-card/internal/deck"
)

// applyLayoffs extends knockerMelds with opponent deadwood cards that attach
// legally. Repeats until no card attaches (an earlier layoff can open a new end).
// laidOff is what moved, in the order it was consumed: reconstructing it afterwards
// by diffing the two hands is guesswork about something this loop already knew.
func applyLayoffs(
	opponentDeadwood []deck.Card, knockerMelds [][]deck.Card,
) (extended [][]deck.Card, remaining, laidOff []deck.Card) {
	extended = cloneMelds(knockerMelds)
	remaining = slices.Clone(opponentDeadwood)

	for changed := true; changed; {
		changed = false
		for i := 0; i < len(remaining); {
			idx, ok := findAttach(remaining[i], extended)
			if !ok {
				i++
				continue
			}
			extended[idx] = append(extended[idx], remaining[i])
			sortMeld(extended[idx])
			laidOff = append(laidOff, remaining[i])
			remaining = slices.Delete(remaining, i, i+1)
			changed = true
		}
	}
	return extended, remaining, laidOff
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
