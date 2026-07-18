package poker

import (
	"sort"
	"terminalcard/internal/deck"
)

type HandRank int

const (
	HighCard HandRank = iota
	Pair
	TwoPair
	ThreeOfAKind
	Straight
	Flush
	FullHouse
	FourOfAKind
	StraightFlush
)

func rankValue(r deck.Rank) int {
	if r == deck.Ace {
		return 14
	}
	return int(r) + 1
}

// EvaluateHand evaluates up to 7 cards and returns a score that can be directly compared
// Score = (HandRank << 20) | (Card1 << 16) | (Card2 << 12) | (Card3 << 8) | (Card4 << 4) | Card5
// Where Card1-5 are the ranks of the 5 cards that make up the hand, in descending order of importance.
func EvaluateHand(cards []deck.Card) int {
	if len(cards) < 5 {
		return 0
	}

	type cardData struct {
		rank int
		suit deck.Suit
	}
	var hand []cardData
	for _, c := range cards {
		hand = append(hand, cardData{rank: rankValue(c.Rank), suit: c.Suit})
	}

	sort.Slice(hand, func(i, j int) bool {
		return hand[i].rank > hand[j].rank
	})

	counts := make(map[int]int)
	suits := make(map[deck.Suit][]int)
	for _, c := range hand {
		counts[c.rank]++
		suits[c.suit] = append(suits[c.suit], c.rank)
	}

	var four, three int
	var pairs []int

	for r := 14; r >= 2; r-- {
		switch counts[r] {
		case 4:
			four = r
		case 3:
			if three == 0 {
				three = r
			} else {
				pairs = append(pairs, r)
			}
		case 2:
			pairs = append(pairs, r)
		}
	}

	var flushRanks []int
	for _, sRanks := range suits {
		if len(sRanks) >= 5 {
			flushRanks = sRanks[:5]
			break
		}
	}

	getStraight := func(ranks []int) int {
		unique := []int{}
		last := -1
		for _, r := range ranks {
			if r != last {
				unique = append(unique, r)
				last = r
			}
		}
		if len(unique) >= 5 {
			for i := 0; i <= len(unique)-5; i++ {
				if unique[i]-unique[i+4] == 4 {
					return unique[i]
				}
			}
			if unique[0] == 14 {
				has5, has4, has3, has2 := false, false, false, false
				for _, r := range unique {
					if r == 5 {
						has5 = true
					}
					if r == 4 {
						has4 = true
					}
					if r == 3 {
						has3 = true
					}
					if r == 2 {
						has2 = true
					}
				}
				if has5 && has4 && has3 && has2 {
					return 5
				}
			}
		}
		return 0
	}

	var allRanks []int
	for _, c := range hand {
		allRanks = append(allRanks, c.rank)
	}
	straightHigh := getStraight(allRanks)

	straightFlushHigh := 0
	var allFlushRanks []int
	for _, sRanks := range suits {
		if len(sRanks) >= 5 {
			allFlushRanks = sRanks
			break
		}
	}
	if allFlushRanks != nil {
		straightFlushHigh = getStraight(allFlushRanks)
	}

	score := func(hr HandRank, r1, r2, r3, r4, r5 int) int {
		return (int(hr) << 20) | (r1 << 16) | (r2 << 12) | (r3 << 8) | (r4 << 4) | r5
	}

	getKickers := func(exclude []int, count int) []int {
		var k []int
		for _, c := range hand {
			excluded := false
			for _, e := range exclude {
				if c.rank == e {
					excluded = true
					break
				}
			}
			if !excluded {
				k = append(k, c.rank)
				if len(k) == count {
					break
				}
			}
		}
		for len(k) < count {
			k = append(k, 0)
		}
		return k
	}

	if straightFlushHigh > 0 {
		return score(StraightFlush, straightFlushHigh, 0, 0, 0, 0)
	}
	if four > 0 {
		k := getKickers([]int{four}, 1)
		return score(FourOfAKind, four, four, four, four, k[0])
	}
	if three > 0 && len(pairs) > 0 {
		return score(FullHouse, three, three, three, pairs[0], pairs[0])
	}
	if flushRanks != nil {
		return score(Flush, flushRanks[0], flushRanks[1], flushRanks[2], flushRanks[3], flushRanks[4])
	}
	if straightHigh > 0 {
		return score(Straight, straightHigh, 0, 0, 0, 0)
	}
	if three > 0 {
		k := getKickers([]int{three}, 2)
		return score(ThreeOfAKind, three, three, three, k[0], k[1])
	}
	if len(pairs) >= 2 {
		k := getKickers([]int{pairs[0], pairs[1]}, 1)
		return score(TwoPair, pairs[0], pairs[0], pairs[1], pairs[1], k[0])
	}
	if len(pairs) == 1 {
		k := getKickers([]int{pairs[0]}, 3)
		return score(Pair, pairs[0], pairs[0], k[0], k[1], k[2])
	}

	k := getKickers(nil, 5)
	return score(HighCard, k[0], k[1], k[2], k[3], k[4])
}
