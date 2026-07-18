package elo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClampRating(t *testing.T) {
	t.Parallel()
	assert.Equal(t, MinRating, ClampRating(-50))
	assert.Equal(t, MinRating, ClampRating(0))
	assert.Equal(t, MaxRating, ClampRating(9000))
	assert.Equal(t, 1500.0, ClampRating(1500))
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
	tests := []struct {
		name     string
		ratingA  float64
		ratingB  float64
		expected float64
	}{
		{"Equal ratings", 1500, 1500, 0.5},
		{"A much higher", 1900, 1500, 0.9090909091},
		{"B much higher", 1500, 1900, 0.0909090909},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpectedScore(tt.ratingA, tt.ratingB)
			assert.InDelta(t, tt.expected, got, 1e-4, "ExpectedScore() mismatch")
		})
	}
}

func TestUpdateRating(t *testing.T) {
	tests := []struct {
		name          string
		rating        float64
		expectedScore float64
		actualScore   float64
		expected      float64
	}{
		{"Win against equal", 1500, 0.5, 1.0, 1516},
		{"Loss against equal", 1500, 0.5, 0.0, 1484},
		{"Max cap enforced", 3990, 0.1, 1.0, 4000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UpdateRating(tt.rating, tt.expectedScore, tt.actualScore)
			assert.InDelta(t, tt.expected, got, 1e-4, "UpdateRating() mismatch")
		})
	}
}

func TestCalculate(t *testing.T) {
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
