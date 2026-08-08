package lobby

import (
	"context"
	"strings"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/lobby"
	"github.com/Pieczasz/terminal-card/internal/player"
	"github.com/Pieczasz/terminal-card/internal/tui/router"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openPublicTable(t *testing.T, m *lobby.Manager, id string, dbID uint, gameName string, opts ...lobby.Option) *lobby.Lobby {
	t.Helper()
	leader := &player.Player{ID: id, DatabaseUser: testUser(dbID, id)}
	opts = append([]lobby.Option{lobby.WithPrivate(false), lobby.WithCardGame(&db.Game{Name: gameName})}, opts...)
	l, err := m.New(leader, opts...)
	require.NoError(t, err)
	return l
}

func newJoinModel(t *testing.T, m *lobby.Manager) *joinModel {
	t.Helper()
	global := router.GlobalContext{
		User:         testUser(99, "browser"),
		LobbyManager: m,
		GameRegistry: testRegistry(),
		Width:        120,
		Height:       40,
	}
	model, ok := NewJoin(global).(*joinModel)
	require.True(t, ok)
	return model
}

// pressJoin sends a key to the browser; press is typed to the in-lobby view.
func pressJoin(t *testing.T, m *joinModel, key string) {
	t.Helper()
	updated, _ := m.Update(keyMsg(key))
	_, ok := updated.(*joinModel)
	require.True(t, ok)
}

// The list is built per refresh, so a new table has to arrive on the tick.
func TestJoin_RefreshPicksUpNewTables(t *testing.T) {
	t.Parallel()
	m := lobby.NewManager(context.Background(), nil)
	view := newJoinModel(t, m)
	require.Empty(t, view.entries, "nothing is open yet")

	openPublicTable(t, m, "host", 1, testGameName)

	updated, cmd := view.Update(refreshMsg{})

	require.NotNil(t, cmd, "the refresh must reschedule itself or the list freezes")
	browser, ok := updated.(*joinModel)
	require.True(t, ok)
	assert.Len(t, browser.entries, 1, "a table opened since the last refresh shows up")
}

// A table in play is not joinable, so it leaves the list.
func TestJoin_RefreshDropsTablesThatStarted(t *testing.T) {
	t.Parallel()
	m := lobby.NewManager(context.Background(), nil)
	l := openPublicTable(t, m, "host", 1, testGameName, lobby.WithMaxPlayers(2))
	guest := &player.Player{ID: "guest", DatabaseUser: testUser(2, "guest")}
	require.NoError(t, m.JoinLobbyByCode(l.Code(), guest))

	view := newJoinModel(t, m)
	view.filter = lobby.BrowseFilter{}
	view.refresh()
	require.Len(t, view.entries, 1)

	registry := testRegistry()
	require.NoError(t, l.ToggleReady(l.Leader(), registry))
	require.NoError(t, l.ToggleReady(guest, registry))

	view.refresh()

	assert.Empty(t, view.entries, "a table in play is not one you can join")
}

// Rows vanish under the cursor, so it is clamped on every refresh.
func TestJoin_CursorSurvivesAShrinkingList(t *testing.T) {
	t.Parallel()
	m := lobby.NewManager(context.Background(), nil)
	tables := make([]*lobby.Lobby, 0, 3)
	for i, name := range []string{"a", "b", "c"} {
		tables = append(tables, openPublicTable(t, m, name, uint(i+1), testGameName))
	}

	view := newJoinModel(t, m)
	require.Len(t, view.entries, 3)
	pressJoin(t, view, "j")
	pressJoin(t, view, "j")
	require.Equal(t, 2, view.cursor)

	for _, l := range tables {
		m.RemoveLobby(l.Code())
	}

	view.refresh()

	assert.Zero(t, view.cursor, "the cursor cannot point past the end of an empty list")
	require.NotPanics(t, func() { view.joinSelected() }, "and joining nothing is a no-op")
}

func TestJoin_FiltersCycle(t *testing.T) {
	t.Parallel()

	// Each case gets its own table set: a filter left set by the previous one would
	// silently change what the next is counting.
	newBrowser := func(t *testing.T) *joinModel {
		t.Helper()
		m := lobby.NewManager(context.Background(), nil)
		openPublicTable(t, m, "poker", 1, "Poker")
		openPublicTable(t, m, "eights", 2, testGameName, lobby.WithRanked(true))
		view := newJoinModel(t, m)
		require.Len(t, view.entries, 2)
		return view
	}

	t.Run("game steps through every game on offer and back to any", func(t *testing.T) {
		t.Parallel()
		view := newBrowser(t)
		games := view.games
		require.Equal(t, []string{testGameName, "Poker"}, games, "sorted, so the cycle is predictable")

		pressJoin(t, view, "g")
		assert.Equal(t, games[0], view.filter.GameName)
		assert.Len(t, view.entries, 1, "narrowed to one game")

		pressJoin(t, view, "g")
		assert.Equal(t, games[1], view.filter.GameName)

		pressJoin(t, view, "g")
		assert.Empty(t, view.filter.GameName, "past the last game it comes back to any")
		assert.Len(t, view.entries, 2)
	})

	t.Run("mode cycles any, ranked, casual", func(t *testing.T) {
		t.Parallel()
		view := newBrowser(t)

		pressJoin(t, view, "m")
		assert.Equal(t, lobby.BrowseRanked, view.filter.Mode)
		require.Len(t, view.entries, 1, "only the ranked table matches")
		assert.True(t, view.entries[0].Ranked)

		pressJoin(t, view, "m")
		assert.Equal(t, lobby.BrowseCasual, view.filter.Mode)
		require.Len(t, view.entries, 1)
		assert.False(t, view.entries[0].Ranked)

		pressJoin(t, view, "m")
		assert.Equal(t, lobby.BrowseAny, view.filter.Mode)
		assert.Len(t, view.entries, 2)
	})

	t.Run("seats toggles between joinable and every table", func(t *testing.T) {
		t.Parallel()
		view := newBrowser(t)
		require.True(t, view.filter.OnlyWithRoom, "the browser opens on joinable tables only")

		pressJoin(t, view, "o")
		assert.False(t, view.filter.OnlyWithRoom)

		pressJoin(t, view, "o")
		assert.True(t, view.filter.OnlyWithRoom)
	})
}

// Waiting for the next tick would read as the key not working.
func TestJoin_FilterAppliesImmediatelyAndResetsTheCursor(t *testing.T) {
	t.Parallel()
	m := lobby.NewManager(context.Background(), nil)
	openPublicTable(t, m, "poker", 1, "Poker")
	openPublicTable(t, m, "eights", 2, testGameName)

	view := newJoinModel(t, m)
	pressJoin(t, view, "j")
	require.Equal(t, 1, view.cursor)

	pressJoin(t, view, "g")

	assert.Zero(t, view.cursor, "the list changed, so the cursor goes back to the top")
	assert.Len(t, view.entries, 1, "and the narrower list is already on screen")
}

// A filter pinned to a game with no tables shows an empty list for no visible reason.
func TestJoin_GameFilterWithNoTablesFallsBackToAny(t *testing.T) {
	t.Parallel()
	m := lobby.NewManager(context.Background(), nil)
	view := newJoinModel(t, m)
	view.filter.GameName = "Poker"

	view.cycleGame()

	assert.Empty(t, view.filter.GameName)
}

// The mode tag is how a player tells ranked from casual before joining.
func TestJoin_ViewShowsTheColumns(t *testing.T) {
	t.Parallel()
	m := lobby.NewManager(context.Background(), nil)
	ranked := openPublicTable(t, m, "poker", 1, "Poker", lobby.WithRanked(true), lobby.WithMaxPlayers(4))

	view := newJoinModel(t, m)
	rendered := stripANSI(view.View().Content)

	assert.Contains(t, rendered, ranked.Code(), "the code is what a player joins by")
	assert.Contains(t, rendered, "Poker")
	assert.Contains(t, rendered, "1/4", "seats taken over seats available")
	assert.Contains(t, rendered, "ranked", "ranked or casual has to be visible before joining")
	assert.Contains(t, rendered, "game: any", "the active filters are on screen")
}

func TestJoin_ViewSaysWhenNothingMatches(t *testing.T) {
	t.Parallel()
	m := lobby.NewManager(context.Background(), nil)
	view := newJoinModel(t, m)

	rendered := stripANSI(view.View().Content)

	assert.Contains(t, rendered, "No tables match")
}

// stripANSI keeps assertions about text rather than the palette.
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
