package elo

import (
	"log/slog"
	"math"
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
//
// NaN would slip through min/max and reach the rankings table as 0. It only arises
// from already-corrupt arithmetic upstream, so it is logged rather than quietly
// reset to a plausible-looking number.
func ClampRating(rating float64) float64 {
	if math.IsNaN(rating) {
		slog.Error("NaN rating clamped to the default", "default", DefaultRating)
		return DefaultRating
	}
	return min(max(rating, MinRating), MaxRating)
}

// ToUint32 converts a rating to a stored Elo value after clamping and rounding.
func ToUint32(rating float64) uint32 {
	return uint32(math.Round(ClampRating(rating)))
}

type Player struct {
	ID     string
	Rating float64
}

// ExpectedScore calculates the expected probability of player A winning against player B.
// Returns a value between 0.0 and 1.0.
func ExpectedScore(ratingA, ratingB float64) float64 {
	return 1.0 / (1.0 + math.Pow(10.0, (ratingB-ratingA)/400.0))
}

// Calculate applies the Simple Multiplayer Elo (SME) algorithm.
// https://www.tckerrigan.com/Misc/Multiplayer_Elo/
// The player slice MUST be sorted by performance, from 1st place (index 0) to last place (index n-1).
// It returns a map of Player ID to their new rating.
func Calculate(players []Player) map[string]float64 {
	n := len(players)
	newRatings := make(map[string]float64, n)

	if n == 0 {
		return newRatings
	}
	if n == 1 {
		newRatings[players[0].ID] = players[0].Rating
		return newRatings
	}

	// SME scores each player against their immediate neighbours only: a win over the
	// one below, a loss to the one above. The two ends have a single comparison.
	for i, player := range players {
		var totalDelta float64

		if i < n-1 {
			opponent := players[i+1]
			expectedWin := ExpectedScore(player.Rating, opponent.Rating)
			deltaWin := KFactor * (1.0 - expectedWin)
			totalDelta += deltaWin
		}

		if i > 0 {
			opponent := players[i-1]
			expectedLoss := ExpectedScore(player.Rating, opponent.Rating)
			deltaLoss := KFactor * (0.0 - expectedLoss)
			totalDelta += deltaLoss
		}

		newRatings[player.ID] = ClampRating(player.Rating + totalDelta)
	}

	return newRatings
}
