package elo

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClampRating(t *testing.T) {
	t.Parallel()
	assert.InDelta(t, MinRating, ClampRating(-50), 1e-9)
	assert.InDelta(t, MinRating, ClampRating(0), 1e-9)
	assert.InDelta(t, MaxRating, ClampRating(9000), 1e-9)
	assert.InDelta(t, 1500.0, ClampRating(1500), 1e-9)
	assert.Equal(t, uint32(100), ToUint32(-1))
	assert.Equal(t, uint32(4000), ToUint32(99999))
}

func TestCalculate_MinFloor(t *testing.T) {
	t.Parallel()
	got := Calculate([]Player{
		{ID: "winner", Rating: 1500},
		{ID: "loser", Rating: MinRating},
	})
	assert.GreaterOrEqual(t, got["loser"], MinRating)
	assert.LessOrEqual(t, got["winner"], MaxRating)
}

func TestExpectedScore(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		ratingA  float64
		ratingB  float64
		expected float64
	}{
		{"Equal ratings", 1500, 1500, 0.5},
		{"A much higher", 1900, 1500, 0.9090909091},
		{"B much higher", 1500, 1900, 0.0909090909},
		{"Underdog by 200", 1400, 1600, 0.2402530734},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExpectedScore(tt.ratingA, tt.ratingB)
			assert.InDelta(t, tt.expected, got, 1e-4, "ExpectedScore() mismatch")
		})
	}
}

func TestCalculate_UnequalRatings(t *testing.T) {
	t.Parallel()

	// Lower-rated "winner" (1400) beats higher-rated "loser" (1600).
	// Sorted best-to-worst: winner first, loser second.
	got := Calculate([]Player{
		{ID: "winner", Rating: 1400},
		{ID: "loser", Rating: 1600},
	})

	const (
		equalGain    = 16.0      // symmetric equal-rating win gains exactly 16.
		expectedWin  = 1424.3119 // 1400 + 32*(1 - E), E = 0.24025.
		expectedLoss = 1575.6881 // 1600 + 32*(0 - (1-E)).
		tolerance    = 1e-3
	)

	winnerDelta := got["winner"] - 1400.0
	loserDelta := got["loser"] - 1600.0

	// The underdog gains more than in the symmetric equal-rating case.
	assert.Greater(t, winnerDelta, equalGain, "underdog should gain more than the equal-rating baseline")

	// Signs are correct: winner up, loser down.
	assert.Positive(t, winnerDelta, "winner should gain rating")
	assert.Negative(t, loserDelta, "loser should lose rating")

	// Magnitudes match the hand-computed values.
	assert.InDelta(t, expectedWin, got["winner"], tolerance, "winner rating mismatch")
	assert.InDelta(t, expectedLoss, got["loser"], tolerance, "loser rating mismatch")

	// Zero-sum: what the winner gains, the loser loses.
	assert.InDelta(t, 0.0, winnerDelta+loserDelta, tolerance, "two-player match must be zero-sum")
}

func TestCalculate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		players  []Player
		expected map[string]float64
	}{
		{
			name:     "0 players",
			players:  []Player{},
			expected: map[string]float64{},
		},
		{
			name: "1 player",
			players: []Player{
				{ID: "p1", Rating: 1500},
			},
			expected: map[string]float64{
				"p1": 1500,
			},
		},
		{
			name: "2 players equal rating",
			players: []Player{
				{ID: "p1", Rating: 1500},
				{ID: "p2", Rating: 1500},
			},
			expected: map[string]float64{
				"p1": 1516,
				"p2": 1484,
			},
		},
		{
			name: "3 players equal rating",
			players: []Player{
				{ID: "p1", Rating: 1500},
				{ID: "p2", Rating: 1500},
				{ID: "p3", Rating: 1500},
			},
			expected: map[string]float64{
				"p1": 1516,
				"p2": 1500,
				"p3": 1484,
			},
		},
		{
			name: "4 players equal rating",
			players: []Player{
				{ID: "p1", Rating: 1500},
				{ID: "p2", Rating: 1500},
				{ID: "p3", Rating: 1500},
				{ID: "p4", Rating: 1500},
			},
			expected: map[string]float64{
				"p1": 1516,
				"p2": 1500,
				"p3": 1500,
				"p4": 1484,
			},
		},
		{
			name: "5 players equal rating",
			players: []Player{
				{ID: "p1", Rating: 1500},
				{ID: "p2", Rating: 1500},
				{ID: "p3", Rating: 1500},
				{ID: "p4", Rating: 1500},
				{ID: "p5", Rating: 1500},
			},
			expected: map[string]float64{
				"p1": 1516,
				"p2": 1500,
				"p3": 1500,
				"p4": 1500,
				"p5": 1484,
			},
		},
		{
			name: "6 players equal rating",
			players: []Player{
				{ID: "p1", Rating: 1500},
				{ID: "p2", Rating: 1500},
				{ID: "p3", Rating: 1500},
				{ID: "p4", Rating: 1500},
				{ID: "p5", Rating: 1500},
				{ID: "p6", Rating: 1500},
			},
			expected: map[string]float64{
				"p1": 1516,
				"p2": 1500,
				"p3": 1500,
				"p4": 1500,
				"p5": 1500,
				"p6": 1484,
			},
		},
		{
			name: "7 players equal rating",
			players: []Player{
				{ID: "p1", Rating: 1500},
				{ID: "p2", Rating: 1500},
				{ID: "p3", Rating: 1500},
				{ID: "p4", Rating: 1500},
				{ID: "p5", Rating: 1500},
				{ID: "p6", Rating: 1500},
				{ID: "p7", Rating: 1500},
			},
			expected: map[string]float64{
				"p1": 1516,
				"p2": 1500,
				"p3": 1500,
				"p4": 1500,
				"p5": 1500,
				"p6": 1500,
				"p7": 1484,
			},
		},
		{
			name: "8 players equal rating",
			players: []Player{
				{ID: "p1", Rating: 1500},
				{ID: "p2", Rating: 1500},
				{ID: "p3", Rating: 1500},
				{ID: "p4", Rating: 1500},
				{ID: "p5", Rating: 1500},
				{ID: "p6", Rating: 1500},
				{ID: "p7", Rating: 1500},
				{ID: "p8", Rating: 1500},
			},
			expected: map[string]float64{
				"p1": 1516,
				"p2": 1500,
				"p3": 1500,
				"p4": 1500,
				"p5": 1500,
				"p6": 1500,
				"p7": 1500,
				"p8": 1484,
			},
		},
		{
			name: "9 players equal rating",
			players: []Player{
				{ID: "p1", Rating: 1500},
				{ID: "p2", Rating: 1500},
				{ID: "p3", Rating: 1500},
				{ID: "p4", Rating: 1500},
				{ID: "p5", Rating: 1500},
				{ID: "p6", Rating: 1500},
				{ID: "p7", Rating: 1500},
				{ID: "p8", Rating: 1500},
				{ID: "p9", Rating: 1500},
			},
			expected: map[string]float64{
				"p1": 1516,
				"p2": 1500,
				"p3": 1500,
				"p4": 1500,
				"p5": 1500,
				"p6": 1500,
				"p7": 1500,
				"p8": 1500,
				"p9": 1484,
			},
		},
		{
			name: "Max Rating Cap",
			players: []Player{
				{ID: "p1", Rating: 3995},
				{ID: "p2", Rating: 1500},
			},
			expected: map[string]float64{
				"p1": 3995.0,
				"p2": 1500.0,
			},
		},
		{
			name: "Max Rating Cap Reachable",
			players: []Player{
				{ID: "p1", Rating: 3995},
				{ID: "p2", Rating: 3995},
			},
			expected: map[string]float64{
				"p1": 4000.0,
				"p2": 3979.0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Calculate(tt.players)
			assert.Len(t, got, len(tt.expected), "Calculate() map size mismatch")
			for id, expectedRating := range tt.expected {
				gotRating, ok := got[id]
				assert.True(t, ok, "Calculate() missing player %s", id)
				if ok {
					assert.InDelta(t, expectedRating, gotRating, 1e-4, "Calculate() mismatch for %s", id)
				}
			}
		})
	}
}

// Calculate runs once per ranked game over every seat, and is O(n) in players with
// a map allocation per call.
func BenchmarkCalculate(b *testing.B) {
	for _, n := range []int{2, 6, 9} {
		b.Run(fmt.Sprintf("players=%d", n), func(b *testing.B) {
			players := make([]Player, 0, n)
			for i := range n {
				players = append(players, Player{ID: strconv.Itoa(i), Rating: DefaultRating + float64(i*25)})
			}
			b.ReportAllocs()
			for b.Loop() {
				_ = Calculate(players)
			}
		})
	}
}
