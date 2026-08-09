package elo

import (
	"cmp"
	"log/slog"
	"math"
	"slices"
)

const (
	DefaultRating float64 = 1500.0
	MinRating     float64 = 100.0
	MaxRating     float64 = 4000.0

	// KFactor determines how much ratings can change in a single match. 32 is the
	// standard chess default; a tiered K (higher for new accounts, lower once
	// established) is the upgrade path if rating volatility becomes a problem.
	KFactor float64 = 32.0
)

// ClampRating bounds a rating to [MinRating, MaxRating].
func ClampRating(rating float64) float64 {
	if math.IsNaN(rating) {
		slog.Error("NaN rating clamped to the default", "default", DefaultRating)
		return DefaultRating
	}
	return min(max(rating, MinRating), MaxRating)
}

func ToUint32(rating float64) uint32 {
	return uint32(math.Round(ClampRating(rating)))
}

type Player struct {
	ID     string
	Rating float64
	// Place is the finishing position, 1 for the winner. Neighbours sharing a place
	// drew and score half a point each way. Left zero every entry is its own place,
	// which is the strict ordering the slice already carries - so a caller that has
	// no tie information keeps the old behaviour.
	Place int
}

func ExpectedScore(ratingA, ratingB float64) float64 {
	return 1.0 / (1.0 + math.Pow(10.0, (ratingB-ratingA)/400.0))
}

// Calculate applies the Simple Multiplayer Elo (SME) algorithm.
// https://www.tckerrigan.com/Misc/Multiplayer_Elo/
// The player slice MUST be sorted by performance, from 1st place (index 0) to last place (index n-1).
// Set Player.Place to record draws; see the field.
// It returns a map of Player ID to their new rating.
func Calculate(players []Player) map[string]float64 {
	n := len(players)
	newRatings := make(map[string]float64, n)

	if n == 0 {
		return newRatings
	}
	if n == 1 {
		newRatings[players[0].ID] = ClampRating(players[0].Rating)
		return newRatings
	}

	ordered := slices.Clone(players)
	normalizeTies(ordered)

	// SME scores each player against their immediate neighbors only: a win over the
	// one below, a loss to the one above. The two ends have a single comparison.
	// Each pair is settled once, as a single transfer, so what one side gains is
	// exactly what the other loses even where a bound truncates it.
	//
	// Every pair is scored from the pre-match ratings, which is what makes this SME
	// and not an order-dependent sweep. Only the bound is live: a player sits in two
	// pairs and can be paid by both, so headroom has to be measured against what the
	// earlier pair already moved. Measuring it against the untouched rating twice
	// lets the two transfers together overshoot, and the final clamp then truncates
	// one side of an exchange the other side already paid - destroying rating instead
	// of moving it. Away from the bounds nothing binds and this is plain SME.
	deltas := make([]float64, n)
	for i := range n - 1 {
		moved := capTransfer(
			rawTransfer(ordered[i], ordered[i+1]),
			ordered[i].Rating+deltas[i],
			ordered[i+1].Rating+deltas[i+1],
		)
		deltas[i] += moved
		deltas[i+1] -= moved
	}

	for i, player := range ordered {
		newRatings[player.ID] = ClampRating(player.Rating + deltas[i])
	}

	return newRatings
}

// normalizeTies orders players sharing a place by rating, highest first, so a draw
// settles the same way whatever order the caller happened to list the tied players
// in. SME only ever compares neighbours, so without this which pairs a tie produces
// - and therefore how much rating it moves - depends on slice position alone.
func normalizeTies(players []Player) {
	for start := 0; start < len(players); {
		end := start + 1
		for end < len(players) && players[end].Place != 0 && players[end].Place == players[start].Place {
			end++
		}
		if end-start > 1 {
			slices.SortFunc(players[start:end], func(a, b Player) int {
				if c := cmp.Compare(b.Rating, a.Rating); c != 0 {
					return c
				}
				return cmp.Compare(a.ID, b.ID)
			})
		}
		start = end
	}
}

// pairTransfer is the rating moved from the lower-placed player to the higher-placed
// one. Tied neighbours score half a point each way, which is how a placement carries
// a draw instead of an invented order.
//
// The transfer is capped by what the pair can actually pay: a player already on
// MinRating has nothing left to lose, one on MaxRating nothing left to win. Clamping
// the results afterwards instead would truncate only one side of the exchange,
// minting rating at the floor and burning it at the ceiling.
func rawTransfer(upper, lower Player) float64 {
	score := 1.0
	if drew(upper, lower) {
		score = 0.5
	}
	return KFactor * (score - ExpectedScore(upper.Rating, lower.Rating))
}

// capTransfer bounds a transfer by what the pair can actually pay, from their live
// ratings. A negative moved is a draw the favourite was expected to win, which pays
// the other way, so the two bounds swap with it.
func capTransfer(moved, gainsRating, losesRating float64) float64 {
	if moved < 0 {
		return -min(-moved, max(MaxRating-losesRating, 0), max(gainsRating-MinRating, 0))
	}
	return min(moved, max(MaxRating-gainsRating, 0), max(losesRating-MinRating, 0))
}

func drew(a, b Player) bool {
	return a.Place != 0 && a.Place == b.Place
}
