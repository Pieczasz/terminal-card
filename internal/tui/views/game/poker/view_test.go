package poker

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seatZones splits the opponents around the table with hand-written slice
// arithmetic per seat count. Poker seats nine, so every branch is reachable, and an
// off-by-one either drops a player from the table or shows one twice.
func TestSeatZones_PlacesEveryOpponentExactlyOnce(t *testing.T) {
	t.Parallel()

	for total := 1; total <= 9; total++ {
		t.Run(fmt.Sprintf("seats=%d", total), func(t *testing.T) {
			t.Parallel()

			m := &Model{seats: make([]Seat, 0, total)}
			for i := range total {
				m.seats = append(m.seats, Seat{PlayerID: fmt.Sprintf("p%d", i), IsHero: i == 0})
			}

			z := m.seatZones()
			placed := make([]Seat, 0, total)
			placed = append(placed, z.left...)
			placed = append(placed, z.top...)
			placed = append(placed, z.right...)

			wantOpponents := total - 1
			require.Len(t, placed, wantOpponents, "every opponent gets exactly one zone")

			seen := map[string]bool{}
			for _, s := range placed {
				assert.False(t, seen[s.PlayerID], "%s placed twice", s.PlayerID)
				seen[s.PlayerID] = true
				assert.False(t, s.IsHero, "the hero is never an opponent")
			}
		})
	}
}

// With no hero seated - a spectator, or state that has not synced yet - everyone is
// an opponent and nobody may be dropped.
func TestSeatZones_WithoutAHeroPlacesEverybody(t *testing.T) {
	t.Parallel()

	m := &Model{seats: []Seat{{PlayerID: "a"}, {PlayerID: "b"}, {PlayerID: "c"}}}
	z := m.seatZones()

	// ElementsMatch, not Len: a duplicated seat standing in for a dropped one keeps
	// the count right while losing a player off the table.
	placed := slices.Concat(z.left, z.top, z.right)
	assert.ElementsMatch(t, m.seats, placed)
}

func TestSeatZones_EmptyTable(t *testing.T) {
	t.Parallel()

	z := (&Model{}).seatZones()
	assert.Empty(t, z.left)
	assert.Empty(t, z.top)
	assert.Empty(t, z.right)
}
