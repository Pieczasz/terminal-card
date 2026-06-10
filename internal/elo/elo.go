package elo

import (
	"math"
)

const (
	DefaultRating float64 = 1500.0
	MaxRating     float64 = 4000.0

	// KFactor determines how much ratings can change in a single match.
	// TODO: check if we can improve this.
	KFactor float64 = 32.0
)

type Player struct {
	ID     string
	Rating float64
}

// ExpectedScore calculates the expected probability of player A winning against player B.
// Returns a value between 0.0 and 1.0.
func ExpectedScore(ratingA, ratingB float64) float64 {
	return 1.0 / (1.0 + math.Pow(10.0, (ratingB-ratingA)/400.0))
}

// UpdateRating calculates the new rating for a player given their actual score and expected score.
func UpdateRating(rating, expectedScore, actualScore float64) float64 {
	newRating := rating + KFactor*(actualScore-expectedScore)
	if newRating > MaxRating {
		return MaxRating
	}
	return newRating
}

// Calculate applies the Simple Multiplayer Elo (SME) algorithm.
// https://www.tckerrigan.com/Misc/Multiplayer_Elo/
// The players slice MUST be sorted by performance, from 1st place (index 0) to last place (index n-1).
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

	for i, player := range players {
		var totalDelta float64

		// If not last place, player wins against the player immediately below them.
		if i < n-1 {
			opponent := players[i+1]
			expectedWin := ExpectedScore(player.Rating, opponent.Rating)
			deltaWin := KFactor * (1.0 - expectedWin)
			totalDelta += deltaWin
		}

		// If not first place, player loses against the player immediately above them.
		if i > 0 {
			opponent := players[i-1]
			expectedLoss := ExpectedScore(player.Rating, opponent.Rating)
			deltaLoss := KFactor * (0.0 - expectedLoss)
			totalDelta += deltaLoss
		}

		newRating := player.Rating + totalDelta

		if newRating > MaxRating {
			newRating = MaxRating
		}

		newRatings[player.ID] = newRating
	}

	return newRatings
}
