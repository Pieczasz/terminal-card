package profile

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"

	lg "charm.land/lipgloss/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errQuery = errors.New("query failed")

func loaded(t *testing.T, msg profileLoadedMsg) model {
	t.Helper()
	global := router.GlobalContext{Theme: styles.NewTheme(true), Width: 100, Height: 40}
	updated, _ := New(global).Update(msg)
	m, ok := updated.(model)
	require.True(t, ok)
	return m
}

func alice() *db.User {
	return &db.User{
		ID:       1,
		Username: "alice",
		Rankings: []db.Ranking{{Elo: 1600, Game: db.Game{Name: "Poker"}}},
	}
}

// The two queries used to share one error field, so a match history that failed
// threw away a profile that had loaded and the screen said the whole profile was
// unreadable. The half that loaded is still worth showing.
func TestUpdate_AFailedHistoryKeepsTheProfile(t *testing.T) {
	t.Parallel()
	m := loaded(t, profileLoadedMsg{user: alice(), historyErr: errQuery})

	require.NotNil(t, m.userProfile, "the profile query succeeded")
	out := stripANSI(m.renderContent(20))

	assert.Contains(t, out, "Profile for: alice")
	assert.Contains(t, out, "Poker", "the rankings came back with the profile")
	assert.Contains(t, out, "1600")
	assert.Contains(t, out, "Unable to load match history.", "and the half that failed says so")
	assert.NotContains(t, out, "Unable to load profile")
}

// A profile that could not be read has nothing to draw, so that failure still
// takes the whole screen.
func TestUpdate_AFailedProfileIsStillAnErrorScreen(t *testing.T) {
	t.Parallel()
	m := loaded(t, profileLoadedMsg{err: errQuery})

	assert.Contains(t, m.renderContent(20), "Unable to load profile")
}

func TestRenderContent_SaysWhenThereAreNoMatchesRatherThanAnError(t *testing.T) {
	t.Parallel()
	m := loaded(t, profileLoadedMsg{user: alice()})

	out := stripANSI(m.renderContent(20))
	assert.Contains(t, out, "No matches for this filter.")
	assert.NotContains(t, out, "Unable to load match history.")
}

func TestFilteredHistory_WinsAndGame(t *testing.T) {
	t.Parallel()
	history := []db.MatchParticipant{
		{Placement: 1, Match: db.Match{Game: db.Game{Name: "Uno"}, Ranked: true}},
		{Placement: 2, Match: db.Match{Game: db.Game{Name: "Uno"}, Ranked: true}},
		{Placement: 1, Match: db.Match{Game: db.Game{Name: "Poker"}, Ranked: true}},
	}
	m := loaded(t, profileLoadedMsg{user: alice(), history: history})

	m.gameFilterIdx = slices.Index(m.gameFilters, "Uno")
	require.NotEqual(t, -1, m.gameFilterIdx)
	m.resultIdx = slices.Index(m.resultFilters, filterWins)

	filtered := m.filteredHistory()
	require.Len(t, filtered, 1)
	assert.Equal(t, 1, filtered[0].Placement)
	assert.Equal(t, "Uno", filtered[0].Match.Game.Name)
}

func TestRenderContent_FilterDoesNotResizeLayout(t *testing.T) {
	t.Parallel()
	history := []db.MatchParticipant{
		{Placement: 1, EloDelta: 12, Match: db.Match{Game: db.Game{Name: "Uno"}, Ranked: true}},
	}
	m := loaded(t, profileLoadedMsg{user: alice(), history: history})

	base := stripANSI(m.renderContent(20))
	m.gameFilterIdx = slices.Index(m.gameFilters, "Crazy Eights")
	require.NotEqual(t, -1, m.gameFilterIdx)
	m.resultIdx = slices.Index(m.resultFilters, filterLosses)
	filtered := stripANSI(m.renderContent(20))

	assert.Equal(t, lg.Width(base), lg.Width(filtered),
		"cycling game/result filters must not change the profile width")
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

// The profile is a full-screen view, so it has to fit the screen at every size the
// app claims to support - a view taller than the terminal is handed to the terminal
// to wrap, and one wrapped row shifts every row under it.
func TestView_FitsTheTerminal(t *testing.T) {
	t.Parallel()

	user := alice()
	user.Rankings = make([]db.Ranking, 0, 5)
	for _, g := range []string{"Poker", "Hearts", "Uno", "Gin Rummy", "Crazy Eights"} {
		user.Rankings = append(user.Rankings, db.Ranking{Elo: 1600, Game: db.Game{Name: g}})
	}
	history := make([]db.MatchParticipant, 0, 50)
	for i := range 50 {
		history = append(history, db.MatchParticipant{
			Placement: (i % 4) + 1,
			Match:     db.Match{Game: db.Game{Name: "Poker"}, Ranked: true},
		})
	}

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
			global := router.GlobalContext{
				Theme: styles.NewTheme(true), Width: size.w, Height: size.h,
			}
			updated, _ := New(global).Update(profileLoadedMsg{user: user, history: history})
			m, ok := updated.(model)
			require.True(t, ok)

			out := m.View().Content

			assert.LessOrEqual(t, lg.Height(out), size.h, "taller than the terminal")
			assert.LessOrEqual(t, lg.Width(out), size.w, "wider than the terminal")
		})
	}
}
