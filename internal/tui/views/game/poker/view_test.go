package poker

import (
	"fmt"
	"slices"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/tui/components"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"
	gameview "github.com/Pieczasz/terminal-card/internal/tui/views/game"

	lg "charm.land/lipgloss/v2"
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
			placed = append(placed, z.Left...)
			placed = append(placed, z.Top...)
			placed = append(placed, z.Right...)

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
	placed := slices.Concat(z.Left, z.Top, z.Right)
	assert.ElementsMatch(t, m.seats, placed)
}

// The mini card used to derive a numeric rank as int(rank)+1, which printed every
// pip card one higher than it was and turned the ace of the deck's 1-based ranks
// into a "2". The label now comes from the table the full card faces use.
func TestRenderMiniCard_PrintsTheRankOnTheCard(t *testing.T) {
	t.Parallel()
	theme := styles.NewTheme(true)

	tests := []struct {
		rank deck.Rank
		want string
	}{
		{deck.Ace, "A"},
		{deck.Two, "2"},
		{deck.Three, "3"},
		{deck.Four, "4"},
		{deck.Five, "5"},
		{deck.Six, "6"},
		{deck.Seven, "7"},
		{deck.Eight, "8"},
		{deck.Nine, "9"},
		{deck.Ten, "10"},
		{deck.Jack, "J"},
		{deck.Queen, "Q"},
		{deck.King, "K"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			got := stripANSI(renderMiniCard(theme, deck.Card{Rank: tt.rank, Suit: deck.Hearts}))
			assert.Equal(t, fmt.Sprintf("[%2s♥]", tt.want), got)
		})
	}
}

// Every mini card is the same width, so a ten on the board does not shift the
// cards beside it by a column.
func TestRenderMiniCard_IsAFixedWidth(t *testing.T) {
	t.Parallel()
	theme := styles.NewTheme(true)

	want := lg.Width(stripANSI(renderMiniCard(theme, deck.Card{Rank: deck.Ace, Suit: deck.Spades})))
	for _, rank := range []deck.Rank{deck.Ten, deck.King, deck.Two} {
		got := lg.Width(stripANSI(renderMiniCard(theme, deck.Card{Rank: rank, Suit: deck.Spades})))
		assert.Equal(t, want, got, "rank %d changes the card width", rank)
	}
}

// A busted seat is dealt no cards. Drawing a fixed pair of backs for it showed a
// hand at a seat that is out of the match.
func TestRenderSeatCards_ShowsNoBacksForASeatWithNoCards(t *testing.T) {
	t.Parallel()

	m := &Model{Session: gameview.Session{Global: router.GlobalContext{Theme: styles.NewTheme(true)}}}
	busted := Seat{Name: "broke", HandSize: 0}

	for _, compact := range []bool{false, true} {
		t.Run(fmt.Sprintf("compact=%v", compact), func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, stripANSI(m.renderSeatCards(busted, compact)))
		})
	}

	dealt := Seat{Name: "live", HandSize: 2}
	assert.NotEmpty(t, stripANSI(m.renderSeatCards(dealt, true)),
		"a seat holding cards still shows them face down")
}

func TestBoardAndHolePlaceholdersMatchCardFootprint(t *testing.T) {
	t.Parallel()
	th := styles.NewTheme(true)
	card := components.RenderCard(th, deck.Card{Rank: deck.Ace, Suit: deck.Spades}, false)
	empty := renderEmptySlot(th)
	back := renderFacedownCard(th)

	assert.Equal(t, lg.Width(card), lg.Width(empty), "board placeholder width")
	assert.Equal(t, lg.Height(card), lg.Height(empty), "board placeholder height")
	assert.Equal(t, lg.Width(card), lg.Width(back), "hole back width")
	assert.Equal(t, lg.Height(card), lg.Height(back), "hole back height")
}

func TestSeatZones_EmptyTable(t *testing.T) {
	t.Parallel()

	z := (&Model{}).seatZones()
	assert.Empty(t, z.Left)
	assert.Empty(t, z.Top)
	assert.Empty(t, z.Right)
}
