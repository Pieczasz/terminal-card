package leaderboard

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"

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

func TestCycleFilter_AdvancesAndClearsRows(t *testing.T) {
	t.Parallel()
	m := board(t, pageSize)
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
	m := board(t, pageSize*2+3) // 3 pages, last short

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
	m := board(t, pageSize) // only page 1 loaded

	next, cmd := m.goPage(1)
	nm := next.(model)
	assert.True(t, nm.loading)
	assert.NotNil(t, cmd, "moving past the loaded window must request more rows")
	assert.Equal(t, pageSize*2, nm.needsFetch(1))
}

func TestGoPage_DoesNotRefetchAShortLastPage(t *testing.T) {
	t.Parallel()
	m := board(t, pageSize*2+3) // pages 0-2 already held; last page short
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
	m := board(t, pageSize)
	m.exhausted = true

	next, cmd := m.goPage(1)
	require.Nil(t, cmd, "an exhausted feed must not re-query")
	assert.Equal(t, 0, next.(model).page)
}

func TestRenderRankings_FixedPageSize(t *testing.T) {
	t.Parallel()
	m := board(t, pageSize+5)
	m.page = 0

	out := stripANSI(m.renderRankings(80))
	assert.Contains(t, out, "player01")
	assert.Contains(t, out, fmt.Sprintf("player%02d", pageSize))
	assert.NotContains(t, out, fmt.Sprintf("player%02d", pageSize+1),
		"page 1 must not spill into page 2")
	assert.Contains(t, out, "page 1/")

	m.page = 1
	out = stripANSI(m.renderRankings(80))
	assert.Contains(t, out, fmt.Sprintf("player%02d", pageSize+1))
	assert.NotContains(t, out, "player01")
	assert.Contains(t, out, "page 2/")
}

func TestNeedsFetch_CapsAtMax(t *testing.T) {
	t.Parallel()
	m := board(t, maxLeaderboardPlayers)
	assert.Equal(t, 0, m.needsFetch(maxLeaderboardPlayers/pageSize),
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
