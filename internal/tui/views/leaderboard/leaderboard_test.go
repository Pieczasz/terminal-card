package leaderboard

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"

	lg "charm.land/lipgloss/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func rankings(n int) []db.Ranking {
	out := make([]db.Ranking, 0, n)
	for i := range n {
		out = append(out, db.Ranking{
			UserID: uint(i + 1),
			Elo:    uint32(2000 - i),
			User:   db.User{Model: gorm.Model{ID: uint(i + 1)}, Username: fmt.Sprintf("player%02d", i+1)},
			Game:   db.Game{Name: "Poker"},
		})
	}
	return out
}

func board(t *testing.T, n int) model {
	t.Helper()
	return model{
		global:   router.GlobalContext{Theme: styles.NewTheme(true), Width: 100, Height: 40},
		rankings: rankings(n),
		filters:  []string{filterAll, "Poker", "Uno"},
	}
}

// boardRows is the page size board() produces, so tests page by what it draws
// instead of assuming the cap.
func boardRows(t *testing.T) int {
	t.Helper()
	return board(t, 0).rowsPerPage()
}

func TestCycleFilter_AdvancesAndClearsRows(t *testing.T) {
	t.Parallel()
	m := board(t, boardRows(t))
	m.filterIndex = 0

	next, cmd := m.cycleFilter(1)
	nm := next.(model)
	assert.Equal(t, 1, nm.filterIndex)
	assert.Equal(t, "Poker", nm.filters[nm.filterIndex])
	assert.Nil(t, nm.rankings, "stale rows must not linger under a new filter")
	assert.Equal(t, 0, nm.page)
	assert.NotNil(t, cmd, "a filter change reloads from the repository")
	assert.Equal(t, "Poker", nm.gameFilter())
}

func TestGoPage_StaysInsideLoadedPages(t *testing.T) {
	t.Parallel()
	m := board(t, boardRows(t)*2+3) // 3 pages, last short

	next, cmd := m.goPage(1)
	require.Nil(t, cmd, "page 2 is already loaded")
	nm := next.(model)
	assert.Equal(t, 1, nm.page)

	next, cmd = nm.goPage(1)
	require.Nil(t, cmd)
	nm = next.(model)
	assert.Equal(t, 2, nm.page)

	next, cmd = nm.goPage(1)
	assert.NotNil(t, cmd, "one more page past a short tail still probes the repository")
	assert.Equal(t, 2, next.(model).page, "page only advances after the fetch lands")
}

func TestGoPage_FetchesWhenTheNextPageIsMissing(t *testing.T) {
	t.Parallel()
	rows := boardRows(t)
	m := board(t, rows) // only page 1 loaded

	next, cmd := m.goPage(1)
	nm := next.(model)
	assert.True(t, nm.loading)
	assert.NotNil(t, cmd, "moving past the loaded window must request more rows")
	assert.Equal(t, rows*2, nm.needsFetch(1))
}

func TestGoPage_DoesNotRefetchAShortLastPage(t *testing.T) {
	t.Parallel()
	m := board(t, boardRows(t)*2+3) // pages 0-2 already held; last page short
	m.page = 1

	next, cmd := m.goPage(1)
	require.Nil(t, cmd, "short last page is already on hand")
	nm := next.(model)
	assert.Equal(t, 2, nm.page)

	_, cmd = nm.goPage(1)
	assert.NotNil(t, cmd, "past the held window still asks once in case more exist")
}

func TestGoPage_StopsWhenExhausted(t *testing.T) {
	t.Parallel()
	m := board(t, boardRows(t))
	m.exhausted = true

	next, cmd := m.goPage(1)
	require.Nil(t, cmd, "an exhausted feed must not re-query")
	assert.Equal(t, 0, next.(model).page)
}

// A page shows exactly rowsPerPage ranks and no more: a window that drew fewer than
// it paged by would skip the difference on every page turn.
func TestRenderRankings_DrawsExactlyOnePage(t *testing.T) {
	t.Parallel()
	rows := boardRows(t)
	m := board(t, rows+5)
	m.page = 0

	out := stripANSI(m.renderRankings(80))
	assert.Contains(t, out, "player01")
	assert.Contains(t, out, fmt.Sprintf("player%02d", rows))
	assert.NotContains(t, out, fmt.Sprintf("player%02d", rows+1),
		"page 1 must not spill into page 2")
	assert.Contains(t, out, "page 1/")

	m.page = 1
	out = stripANSI(m.renderRankings(80))
	assert.Contains(t, out, fmt.Sprintf("player%02d", rows+1))
	assert.NotContains(t, out, "player01")
	assert.Contains(t, out, "page 2/")
}

func TestNeedsFetch_CapsAtMax(t *testing.T) {
	t.Parallel()
	m := board(t, maxLeaderboardPlayers)
	assert.Equal(t, 0, m.needsFetch(maxLeaderboardPlayers/maxRowsPerPage),
		"a full window does not ask the repository again")
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

// The board is a full-screen view, so it has to fit the screen. It used to force
// twenty rows plus chrome at every size, which overran a stock 80x24 terminal - and
// TooSmall reports 80x24 as perfectly fine, so nothing anywhere caught it.
func TestView_FitsTheTerminalAtEverySupportedSize(t *testing.T) {
	t.Parallel()
	for _, size := range []struct {
		name string
		w, h int
	}{
		{"the declared minimum", styles.MinWidth, styles.MinHeight},
		{"a stock terminal", 80, 24},
		{"a tall terminal", 120, 50},
	} {
		t.Run(size.name, func(t *testing.T) {
			t.Parallel()
			m := model{
				global:   router.GlobalContext{Theme: styles.NewTheme(true), Width: size.w, Height: size.h},
				rankings: rankings(maxLeaderboardPlayers),
				filters:  []string{filterAll, "Poker"},
			}

			out := m.View().Content

			assert.LessOrEqual(t, lg.Height(out), size.h, "the board is taller than the terminal")
			assert.LessOrEqual(t, lg.Width(out), size.w, "the board is wider than the terminal")
		})
	}
}

// Paging must move by exactly what is drawn: a page that steps twenty while showing
// six silently skips fourteen players.
func TestRowsPerPage_PagesByWhatItDraws(t *testing.T) {
	t.Parallel()
	short := model{
		global:   router.GlobalContext{Theme: styles.NewTheme(true), Width: 80, Height: 24},
		rankings: rankings(maxLeaderboardPlayers),
		filters:  []string{filterAll},
	}
	tall := short
	tall.global.Height = 50

	shortRows, tallRows := short.rowsPerPage(), tall.rowsPerPage()
	assert.Less(t, shortRows, maxRowsPerPage, "a stock terminal cannot hold a full page")
	assert.GreaterOrEqual(t, shortRows, minRowsPerPage)
	assert.Equal(t, maxRowsPerPage, tallRows, "a tall terminal still gets the full page")

	// Page 2 starts where page 1 stopped drawing, at both sizes.
	next, _ := short.goPage(1)
	assert.Equal(t, 1, next.(model).page)
	assert.Contains(t, next.(model).renderRankings(styles.InnerWidth(80)),
		fmt.Sprintf("ranks %d-%d", shortRows+1, shortRows*2))
}
