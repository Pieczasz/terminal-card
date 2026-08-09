package ginrummy

import (
	"math"
	"slices"

	"github.com/Pieczasz/terminal-card/internal/deck"
)

func deadwoodPoints(c deck.Card) int { return deck.PipValue(c.Rank) }

func sumDeadwood(cards []deck.Card) int {
	total := 0
	for _, c := range cards {
		total += deadwoodPoints(c)
	}
	return total
}

// BestMeldSplit partitions hand into melds minimizing deadwood points.
func BestMeldSplit(hand []deck.Card) (melds [][]deck.Card, deadwood []deck.Card, deadwoodPts int) {
	n := len(hand)
	if n == 0 {
		return nil, nil, 0
	}
	cards := slices.Clone(hand)
	candidates := generateMeldMasks(cards)

	bestPts := math.MaxInt
	var bestMasks []uint16

	var search func(start int, used uint16, chosen []uint16)
	search = func(start int, used uint16, chosen []uint16) {
		pts := 0
		for i := range n {
			if used&(1<<i) == 0 {
				pts += deadwoodPoints(cards[i])
			}
		}
		if pts < bestPts {
			bestPts = pts
			bestMasks = slices.Clone(chosen)
		}
		if pts == 0 {
			return
		}
		for i := start; i < len(candidates); i++ {
			m := candidates[i]
			if used&m != 0 {
				continue
			}
			search(i+1, used|m, append(chosen, m))
		}
	}
	search(0, 0, nil)

	melds = make([][]deck.Card, 0, len(bestMasks))
	var used uint16
	for _, mask := range bestMasks {
		meld := make([]deck.Card, 0, 4)
		for i := range n {
			if mask&(1<<i) != 0 {
				meld = append(meld, cards[i])
				used |= 1 << i
			}
		}
		melds = append(melds, meld)
	}
	deadwood = make([]deck.Card, 0, n)
	for i := range n {
		if used&(1<<i) == 0 {
			deadwood = append(deadwood, cards[i])
		}
	}
	return melds, deadwood, bestPts
}

func generateMeldMasks(cards []deck.Card) []uint16 {
	masks := append(setMasks(cards), runMasks(cards)...)
	// setMasks and runMasks walk maps, and BestMeldSplit keeps the first split it
	// finds at the best score. Without an order the melds a knock is scored on
	// change between runs on the same hand.
	slices.Sort(masks)
	return masks
}

func setMasks(cards []deck.Card) []uint16 {
	byRank := map[deck.Rank][]int{}
	for i, c := range cards {
		byRank[c.Rank] = append(byRank[c.Rank], i)
	}
	var out []uint16
	for _, idxs := range byRank {
		if len(idxs) < 3 {
			continue
		}
		for size := 3; size <= len(idxs) && size <= 4; size++ {
			for _, combo := range combinations(idxs, size) {
				var mask uint16
				for _, i := range combo {
					mask |= 1 << i
				}
				out = append(out, mask)
			}
		}
	}
	return out
}

func runMasks(cards []deck.Card) []uint16 {
	bySuit := map[deck.Suit][]int{}
	for i, c := range cards {
		bySuit[c.Suit] = append(bySuit[c.Suit], i)
	}
	out := make([]uint16, 0, len(cards))
	for _, idxs := range bySuit {
		slices.SortFunc(idxs, func(a, b int) int {
			return deck.RunOrder(cards[a].Rank) - deck.RunOrder(cards[b].Rank)
		})
		out = append(out, runMasksInSuit(cards, idxs)...)
	}
	return out
}

func runMasksInSuit(cards []deck.Card, idxs []int) []uint16 {
	var out []uint16
	start := 0
	for start < len(idxs) {
		end := start + 1
		for end < len(idxs) &&
			deck.RunOrder(cards[idxs[end]].Rank) == deck.RunOrder(cards[idxs[end-1]].Rank)+1 {
			end++
		}
		out = append(out, subRunMasks(idxs[start:end])...)
		start = end
	}
	return out
}

func subRunMasks(block []int) []uint16 {
	if len(block) < 3 {
		return nil
	}
	var out []uint16
	for i := range block {
		for j := i + 3; j <= len(block); j++ {
			var mask uint16
			for _, idx := range block[i:j] {
				mask |= 1 << idx
			}
			out = append(out, mask)
		}
	}
	return out
}

func combinations(items []int, k int) [][]int {
	if k > len(items) || k <= 0 {
		return nil
	}
	var out [][]int
	var walk func(start int, cur []int)
	walk = func(start int, cur []int) {
		if len(cur) == k {
			out = append(out, slices.Clone(cur))
			return
		}
		for i := start; i < len(items); i++ {
			walk(i+1, append(cur, items[i]))
		}
	}
	walk(0, nil)
	return out
}

func highestPointCard(cards []deck.Card) deck.Card {
	best := cards[0]
	bestPts := deadwoodPoints(best)
	for _, c := range cards[1:] {
		if p := deadwoodPoints(c); p > bestPts {
			best = c
			bestPts = p
		}
	}
	return best
}

func isSet(meld []deck.Card) bool {
	if len(meld) < 3 || len(meld) > 4 {
		return false
	}
	rank := meld[0].Rank
	for _, c := range meld[1:] {
		if c.Rank != rank {
			return false
		}
	}
	return true
}

func isRun(meld []deck.Card) bool {
	if len(meld) < 3 {
		return false
	}
	sorted := slices.Clone(meld)
	slices.SortFunc(sorted, func(a, b deck.Card) int {
		return deck.RunOrder(a.Rank) - deck.RunOrder(b.Rank)
	})
	suit := sorted[0].Suit
	for i := 1; i < len(sorted); i++ {
		if sorted[i].Suit != suit {
			return false
		}
		if deck.RunOrder(sorted[i].Rank) != deck.RunOrder(sorted[i-1].Rank)+1 {
			return false
		}
	}
	return true
}
