package poker

import (
	"cmp"
	"slices"

	"github.com/Pieczasz/terminal-card/internal/deck"
)

type HandRank int

const (
	RankUnknown HandRank = iota
	HighCard
	Pair
	TwoPair
	ThreeOfAKind
	Straight
	Flush
	FullHouse
	FourOfAKind
	StraightFlush
)

const (
	handRankShift = 20
	kickerShift0  = 16
	kickerShift1  = 12
	kickerShift2  = 8
	kickerShift3  = 4
)

// HandValue is a classified poker hand with kickers for tiebreaking.
type HandValue struct {
	Rank    HandRank
	Kickers [5]int // descending importance; unused slots are 0
}

// Score packs Rank and Kickers into a comparable integer.
// Encoding: (HandRank << 20) | (k0 << 16) | (k1 << 12) | (k2 << 8) | (k3 << 4) | k4.
func (h HandValue) Score() int {
	return (int(h.Rank) << handRankShift) |
		(h.Kickers[0] << kickerShift0) |
		(h.Kickers[1] << kickerShift1) |
		(h.Kickers[2] << kickerShift2) |
		(h.Kickers[3] << kickerShift3) |
		h.Kickers[4]
}

func handValue(rank HandRank, kickers ...int) HandValue {
	var hv HandValue
	hv.Rank = rank
	for i := 0; i < len(kickers) && i < 5; i++ {
		hv.Kickers[i] = kickers[i]
	}
	return hv
}

type rankedCard struct {
	rank int
	suit deck.Suit
}

// rankValue is not joker-aware: deck.Joker is 13, so it maps to 14 and is
// indistinguishable from an ace. Harmless today because StandardDeck is the only deck
// builder and it stops at King, but dealing jokers needs wildness taught to
// rankCounts, straightHigh and bestFlush, not just to this function.
func rankValue(r deck.Rank) int {
	if r == deck.Ace {
		return 14
	}
	return int(r) + 1
}

// EvaluateHand evaluates up to 7 cards and returns a score that can be directly compared.
func EvaluateHand(cards []deck.Card) int {
	return ClassifyHand(cards).Score()
}

// ClassifyHand returns the best 5-card hand value from the given cards.
// Hands with fewer than 5 cards yield a zero HandValue (Score 0).
func ClassifyHand(cards []deck.Card) HandValue {
	if len(cards) < 5 {
		return HandValue{}
	}
	hand := normalizeCards(cards)
	quad, trip, pairs := rankCounts(hand)
	return classify(hand, quad, trip, pairs, bestFlush(hand))
}

func normalizeCards(cards []deck.Card) []rankedCard {
	hand := make([]rankedCard, 0, len(cards))
	for _, c := range cards {
		hand = append(hand, rankedCard{rank: rankValue(c.Rank), suit: c.Suit})
	}
	slices.SortFunc(hand, func(a, b rankedCard) int {
		return cmp.Compare(b.rank, a.rank)
	})
	return hand
}

func rankCounts(hand []rankedCard) (quadRank, tripRank int, pairs []int) {
	counts := make(map[int]int, len(hand))
	for _, c := range hand {
		counts[c.rank]++
	}
	for r := 14; r >= 2; r-- {
		switch counts[r] {
		case 4:
			quadRank = r
		case 3:
			if tripRank == 0 {
				tripRank = r
			} else {
				pairs = append(pairs, r)
			}
		case 2:
			pairs = append(pairs, r)
		}
	}
	return quadRank, tripRank, pairs
}

// bestFlush returns the ranks (desc) of the strongest flush, or nil if there is
// none.
//
// The suits are walked in a fixed order and compared, rather than returning the
// first qualifying entry of the map: with 7 cards only one suit can ever reach five,
// but on a larger hand two can, and map iteration order would then make the hand's
// score differ between runs for identical input.
//
// NoSuit is deliberately absent: it marks a joker, and five jokers are not a flush.
func bestFlush(hand []rankedCard) []int {
	suits := make(map[deck.Suit][]int, 4)
	for _, c := range hand {
		suits[c.suit] = append(suits[c.suit], c.rank)
	}

	var best []int
	for _, suit := range []deck.Suit{deck.Spades, deck.Hearts, deck.Diamonds, deck.Clubs} {
		ranks := suits[suit]
		if len(ranks) < 5 {
			continue
		}
		if best == nil || slices.Compare(ranks[:5], best[:5]) > 0 {
			best = ranks
		}
	}
	return best
}

func allRanks(hand []rankedCard) []int {
	ranks := make([]int, len(hand))
	for i, c := range hand {
		ranks[i] = c.rank
	}
	return ranks
}

// straightHigh returns the high card of the best straight in ranks, or 0.
// Wheel (A-2-3-4-5) returns 5.
func straightHigh(ranks []int) int {
	unique := make([]int, 0, len(ranks))
	last := -1
	for _, r := range ranks {
		if r != last {
			unique = append(unique, r)
			last = r
		}
	}
	if len(unique) < 5 {
		return 0
	}
	for i := 0; i <= len(unique)-5; i++ {
		if unique[i]-unique[i+4] == 4 {
			return unique[i]
		}
	}
	if isWheel(unique) {
		return 5
	}
	return 0
}

var wheelLowRanks = []int{5, 4, 3, 2}

func isWheel(unique []int) bool {
	if len(unique) == 0 || unique[0] != 14 {
		return false
	}
	for _, need := range wheelLowRanks {
		if !slices.Contains(unique, need) {
			return false
		}
	}
	return true
}

func kickers(hand []rankedCard, exclude []int, count int) []int {
	k := make([]int, 0, count)
	for _, c := range hand {
		if slices.Contains(exclude, c.rank) {
			continue
		}
		k = append(k, c.rank)
		if len(k) == count {
			break
		}
	}
	for len(k) < count {
		k = append(k, 0)
	}
	return k
}

func classify(hand []rankedCard, quadRank, tripRank int, pairs []int, flushRanks []int) HandValue {
	straightFlushHigh := 0
	if flushRanks != nil {
		straightFlushHigh = straightHigh(flushRanks)
	}
	straight := straightHigh(allRanks(hand))

	switch {
	case straightFlushHigh > 0:
		return handValue(StraightFlush, straightFlushHigh)
	case quadRank > 0:
		k := kickers(hand, []int{quadRank}, 1)
		return handValue(FourOfAKind, quadRank, quadRank, quadRank, quadRank, k[0])
	case tripRank > 0 && len(pairs) > 0:
		return handValue(FullHouse, tripRank, tripRank, tripRank, pairs[0], pairs[0])
	case flushRanks != nil:
		return handValue(Flush, flushRanks[0], flushRanks[1], flushRanks[2], flushRanks[3], flushRanks[4])
	case straight > 0:
		return handValue(Straight, straight)
	case tripRank > 0:
		k := kickers(hand, []int{tripRank}, 2)
		return handValue(ThreeOfAKind, tripRank, tripRank, tripRank, k[0], k[1])
	case len(pairs) >= 2:
		k := kickers(hand, []int{pairs[0], pairs[1]}, 1)
		return handValue(TwoPair, pairs[0], pairs[0], pairs[1], pairs[1], k[0])
	case len(pairs) == 1:
		k := kickers(hand, []int{pairs[0]}, 3)
		return handValue(Pair, pairs[0], pairs[0], k[0], k[1], k[2])
	default:
		k := kickers(hand, nil, 5)
		return handValue(HighCard, k[0], k[1], k[2], k[3], k[4])
	}
}
