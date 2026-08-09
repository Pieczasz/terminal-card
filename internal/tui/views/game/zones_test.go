package game

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Uno seats ten and poker nine, so a layout that runs out of slots leaves players
// off the table. Every opponent has to land in exactly one zone at every table size.
func TestSplitZones_PlacesEveryOpponentExactlyOnce(t *testing.T) {
	t.Parallel()

	for n := range 12 {
		t.Run(fmt.Sprintf("opponents=%d", n), func(t *testing.T) {
			t.Parallel()

			opponents := make([]string, 0, n)
			for i := range n {
				opponents = append(opponents, fmt.Sprintf("p%d", i))
			}

			z := SplitZones(opponents)
			placed := slices.Concat(z.Left, z.Top, z.Right)

			// ElementsMatch, not Len: a seat drawn twice would keep the count right
			// while losing a different player off the table.
			assert.ElementsMatch(t, opponents, placed)
			assert.LessOrEqual(t, len(z.Top), topCapacity, "the top row has to fit across a terminal")
		})
	}
}

// The sides carry the overflow, so neither of them may run away with the table.
func TestSplitZones_BalancesTheSides(t *testing.T) {
	t.Parallel()

	for n := 2; n <= 12; n++ {
		opponents := make([]int, n)
		z := SplitZones(opponents)
		assert.Lenf(t, z.Left, len(z.Right), "%d opponents: the sides have to match", n)
	}
}

func TestSplitZones_EmptyTable(t *testing.T) {
	t.Parallel()

	z := SplitZones([]string{})
	assert.Empty(t, z.Left)
	assert.Empty(t, z.Top)
	assert.Empty(t, z.Right)
}

// The name and the hand count are what a player reads off somebody else's seat, so
// both have to be on screen for every opponent however many are seated.
func TestRenderOpponentEdges_ShowsEverySeat(t *testing.T) {
	t.Parallel()
	theme := styles.NewTheme(true)

	for _, seats := range []int{1, 2, 3, 6, 9} {
		t.Run(fmt.Sprintf("opponents=%d", seats), func(t *testing.T) {
			t.Parallel()

			base := BaseState{Opponents: make([]game.PlayerSnapshot, 0, seats)}
			for i := range seats {
				base.Opponents = append(base.Opponents, game.PlayerSnapshot{
					ID:       fmt.Sprintf("p%d", i),
					Username: fmt.Sprintf("player%d", i),
					HandSize: i + 1,
				})
			}

			edges := RenderOpponentEdges(theme, base, false)
			rendered := stripANSI(strings.Join([]string{edges.Top, edges.Left, edges.Right}, "\n"))

			for _, o := range base.Opponents {
				assert.Containsf(t, rendered, o.Username, "%s is not on the table", o.Username)
				assert.Containsf(t, rendered, fmt.Sprintf("[%d cards]", o.HandSize),
					"%s's hand count is missing", o.Username)
			}
		})
	}
}

// The seat on turn is the one a player is waiting on, and it is named by ID:
// two players may share a display name.
func TestRenderOpponentEdges_MarksTheSeatOnTurn(t *testing.T) {
	t.Parallel()
	theme := styles.NewTheme(true)

	base := BaseState{
		CurrentPlayerID: "p1",
		Opponents: []game.PlayerSnapshot{
			{ID: "p0", Username: "same", HandSize: 3},
			{ID: "p1", Username: "same", HandSize: 3},
		},
	}

	turn := RenderOpponentEdges(theme, base, false)
	base.CurrentPlayerID = "p0"
	other := RenderOpponentEdges(theme, base, false)

	require.NotEqual(t, turn.Left, other.Left,
		"the highlight has to follow the player ID, not the name they share")
}

func stripANSI(s string) string {
	var out strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEscape = true
		case inEscape && (r == 'm' || r == 'K' || r == 'H'):
			inEscape = false
		case !inEscape:
			out.WriteRune(r)
		}
	}
	return out.String()
}
