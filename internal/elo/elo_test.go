package elo

import (
	"bytes"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClampRating(t *testing.T) {
	t.Parallel()
	// NaN is unorderable, so min/max would pass it through to the rankings table.
	assert.InDelta(t, DefaultRating, ClampRating(math.NaN()), 1e-9, "NaN is treated as unrated")
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

func TestCalculate_ConservesRatingAtTheBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		players []Player
	}{
		{
			name: "loser already on the floor",
			players: []Player{
				{ID: "winner", Rating: 1500},
				{ID: "loser", Rating: MinRating},
			},
		},
		{
			name: "winner already on the ceiling",
			players: []Player{
				{ID: "winner", Rating: MaxRating},
				{ID: "loser", Rating: 1500},
			},
		},
		{
			name: "both within a few points of the ceiling",
			players: []Player{
				{ID: "winner", Rating: MaxRating - 5},
				{ID: "loser", Rating: MaxRating - 5},
			},
		},
		{
			name: "a whole field pinned to the floor",
			players: []Player{
				{ID: "p1", Rating: MinRating},
				{ID: "p2", Rating: MinRating},
				{ID: "p3", Rating: MinRating + 3},
			},
		},
		{
			name: "an ordinary mid-table field",
			players: []Player{
				{ID: "p1", Rating: 1800},
				{ID: "p2", Rating: 1500},
				{ID: "p3", Rating: 1200},
				{ID: "p4", Rating: 900},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Calculate(tt.players)

			var before, after float64
			for _, p := range tt.players {
				before += p.Rating
				assert.GreaterOrEqual(t, got[p.ID], MinRating, "%s fell through the floor", p.ID)
				assert.LessOrEqual(t, got[p.ID], MaxRating, "%s broke the ceiling", p.ID)
				after += got[p.ID]
			}
			assert.InDelta(t, before, after, 1e-9, "the table's total rating must not change")
		})
	}
}

func TestCalculate_FlooredLoserPaysNothing(t *testing.T) {
	t.Parallel()
	got := Calculate([]Player{
		{ID: "winner", Rating: 2000},
		{ID: "loser", Rating: MinRating},
	})

	assert.InDelta(t, 2000.0, got["winner"], 1e-9, "there is no rating to take")
	assert.InDelta(t, MinRating, got["loser"], 1e-9)
}

// The mirror image: a winner on the ceiling cannot take anything, so the loser
// keeps it rather than having it deleted from the pool.
func TestCalculate_CappedWinnerBurnsNothing(t *testing.T) {
	t.Parallel()
	got := Calculate([]Player{
		{ID: "winner", Rating: MaxRating},
		{ID: "loser", Rating: 1500},
	})

	assert.InDelta(t, MaxRating, got["winner"], 1e-9)
	assert.InDelta(t, 1500.0, got["loser"], 1e-9, "nobody gained it, so nobody loses it")
}

func TestCalculate_DrawMovesRatingTowardsTheUnderdog(t *testing.T) {
	t.Parallel()
	got := Calculate([]Player{
		{ID: "favourite", Rating: 1900, Place: 1},
		{ID: "underdog", Rating: 1300, Place: 1},
	})

	assert.Less(t, got["favourite"], 1900.0, "a draw is a bad day for the favourite")
	assert.Greater(t, got["underdog"], 1300.0)
	assert.InDelta(t, 3200.0, got["favourite"]+got["underdog"], 1e-9, "a draw is still zero-sum")
}

func TestCalculate_ZeroPlacesAreNotAllDraws(t *testing.T) {
	t.Parallel()
	got := Calculate([]Player{
		{ID: "p1", Rating: 1500},
		{ID: "p2", Rating: 1500},
	})

	assert.InDelta(t, 1516.0, got["p1"], 1e-9)
	assert.InDelta(t, 1484.0, got["p2"], 1e-9)
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
			got := expectedScore(tt.ratingA, tt.ratingB)
			assert.InDelta(t, tt.expected, got, 1e-4, "expectedScore() mismatch")
		})
	}
}

func TestCalculate_UnequalRatings(t *testing.T) {
	t.Parallel()

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

	assert.Greater(t, winnerDelta, equalGain, "underdog should gain more than the equal-rating baseline")
	assert.Positive(t, winnerDelta, "winner should gain rating")
	assert.Negative(t, loserDelta, "loser should lose rating")
	assert.InDelta(t, expectedWin, got["winner"], tolerance, "winner rating mismatch")
	assert.InDelta(t, expectedLoss, got["loser"], tolerance, "loser rating mismatch")
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
				"p2": 3990.0,
			},
		},
		{
			name: "Tied neighbours split the point",
			players: []Player{
				{ID: "p1", Rating: 1500, Place: 1},
				{ID: "p2", Rating: 1500, Place: 1},
			},
			expected: map[string]float64{
				"p1": 1500.0,
				"p2": 1500.0,
			},
		},
		{
			name: "A tie inside the field keeps the places around it",
			players: []Player{
				{ID: "p1", Rating: 1500, Place: 1},
				{ID: "p2", Rating: 1500, Place: 2},
				{ID: "p3", Rating: 1500, Place: 2},
			},
			expected: map[string]float64{
				"p1": 1516.0,
				"p2": 1484.0,
				"p3": 1500.0,
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

func FuzzToUint32(f *testing.F) {
	f.Add(1500.0)
	f.Add(math.NaN())
	f.Add(math.Inf(1))
	f.Add(math.Inf(-1))
	f.Add(math.Copysign(0, -1)) // negative zero; -0.0 is just 0.0 in Go

	f.Fuzz(func(t *testing.T, rating float64) {
		got := ToUint32(rating)
		assert.GreaterOrEqual(t, got, uint32(MinRating), "stored Elo must not fall below MinRating")
		assert.LessOrEqual(t, got, uint32(MaxRating), "stored Elo must not exceed MaxRating")
	})
}

func TestCalculate_TiesAreOrderIndependent(t *testing.T) {
	t.Parallel()

	forward := []Player{
		{ID: "a", Rating: 1600, Place: 1},
		{ID: "b", Rating: 1500, Place: 1},
		{ID: "c", Rating: 1400, Place: 1},
	}
	shuffled := []Player{
		{ID: "a", Rating: 1600, Place: 1},
		{ID: "c", Rating: 1400, Place: 1},
		{ID: "b", Rating: 1500, Place: 1},
	}

	got, other := Calculate(forward), Calculate(shuffled)
	for _, id := range []string{"a", "b", "c"} {
		assert.InDelta(t, got[id], other[id], 1e-9,
			"a draw must settle the same however the tied players were ordered")
	}

	var net float64
	for _, p := range forward {
		net += got[p.ID] - p.Rating
	}
	assert.InDelta(t, 0.0, net, 1e-9, "a reordered draw must still only transfer rating")
}

// Calculate keys its result by player ID, so two seats sharing one collapse into a
// single entry and one of the two rating changes is thrown away. The signature has
// nowhere to report that, and the repository path builds the slice from a set of
// accounts, so this pins the behaviour rather than defending against it: if the
// collapsing ever becomes reachable, this is the test that has to change with it.
//
//nolint:paralleltest // slog.SetDefault is process-wide
func TestCalculate_DuplicateIDsCollapse(t *testing.T) {
	var logged bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { slog.SetDefault(original) })

	players := []Player{
		{ID: "dup", Rating: 1500, Place: 1},
		{ID: "other", Rating: 1500, Place: 2},
		{ID: "dup", Rating: 1500, Place: 3},
	}

	got := Calculate(players)

	assert.Len(t, got, 2, "three seats, two ids: one result is lost")
	assert.Contains(t, got, "dup")
	assert.Contains(t, got, "other")
	assert.Contains(t, logged.String(), "duplicate player id",
		"a lost rating change has to be visible somewhere")
}
