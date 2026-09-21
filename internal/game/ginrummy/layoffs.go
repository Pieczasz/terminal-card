package ginrummy

import (
	"slices"

	"github.com/Pieczasz/terminal-card/internal/deck"
)

// applyLayoffs lays opponent deadwood off onto knockerMelds, repeating until no card
// attaches: an earlier layoff can open a new end. The grown melds are scaffolding for
// that loop and nothing reads them afterwards, so only the two answers a score needs
// come back. laidOff is what moved, in the order it was consumed: reconstructing it by
// diffing the two hands is guesswork about something this loop already knew.
//
// knockerMelds is cloned: the caller's copy is the knocker's scored arrangement and
// appending to it in place would grow the melds the hand is settled on.
func applyLayoffs(
	opponentDeadwood []deck.Card, knockerMelds [][]deck.Card,
) (remaining, laidOff []deck.Card) {
	melds := cloneMelds(knockerMelds)
	remaining = slices.Clone(opponentDeadwood)

	for changed := true; changed; {
		changed = false
		for i := 0; i < len(remaining); {
			idx, ok := findAttach(remaining[i], melds)
			if !ok {
				i++
				continue
			}
			melds[idx] = append(melds[idx], remaining[i])
			laidOff = append(laidOff, remaining[i])
			remaining = slices.Delete(remaining, i, i+1)
			changed = true
		}
	}
	return remaining, laidOff
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
