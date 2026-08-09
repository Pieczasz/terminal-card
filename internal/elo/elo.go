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
	Place  int
}

func ExpectedScore(ratingA, ratingB float64) float64 {
	return 1.0 / (1.0 + math.Pow(10.0, (ratingB-ratingA)/400.0))
}

// Calculate applies the Simple Multiplayer Elo (SME) algorithm.
// https://www.tckerrigan.com/Misc/Multiplayer_Elo/
// The player slice MUST be sorted by performance, from 1st place (index 0) to last place (index n-1).
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

func rawTransfer(upper, lower Player) float64 {
	score := 1.0
	if drew(upper, lower) {
		score = 0.5
	}
	return KFactor * (score - ExpectedScore(upper.Rating, lower.Rating))
}

func capTransfer(moved, gainsRating, losesRating float64) float64 {
	if moved < 0 {
		return -min(-moved, max(MaxRating-losesRating, 0), max(gainsRating-MinRating, 0))
	}
	return min(moved, max(MaxRating-gainsRating, 0), max(losesRating-MinRating, 0))
}

func drew(a, b Player) bool {
	return a.Place != 0 && a.Place == b.Place
}
