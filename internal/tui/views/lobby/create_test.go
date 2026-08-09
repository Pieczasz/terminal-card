package lobby

import (
	"testing"

	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/game/crazyeight"
	"github.com/Pieczasz/terminal-card/internal/game/poker"
	"github.com/Pieczasz/terminal-card/internal/tui/router"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bothGamesRegistry offers Poker (max 9) and Crazy Eights (max 6), so switching
// between them exercises the clamp.
func bothGamesRegistry(t *testing.T) *game.Registry {
	t.Helper()
	r := game.NewRegistry()
	r.RegisterModule(game.Module{
		Name: "Poker", Slug: "poker",
		Factory: func() game.Rules { return &poker.Rules{} },
	})
	r.RegisterModule(game.Module{
		Name: "Crazy Eights", Slug: "crazy_eights",
		Factory: func() game.Rules { return &crazyeight.Rules{} },
	})
	return r
}

func newCreateModel(t *testing.T) *createModel {
	t.Helper()
	global := router.GlobalContext{
		User:         testUser(1, "alice"),
		GameRegistry: bothGamesRegistry(t),
		Width:        120,
		Height:       40,
	}
	m, ok := NewCreate(global).(*createModel)
	require.True(t, ok)
	return m
}

// Switching from a game that allows 9 players to one that allows 6 must pull the
// setting down, or the lobby is created with a size its own rules reject.
func TestCreate_SwitchingGameClampsMaxPlayers(t *testing.T) {
	t.Parallel()
	m := newCreateModel(t)

	pokerIdx, crazyIdx := -1, -1
	for i, name := range m.gameOptions {
		switch name {
		case "Poker":
			pokerIdx = i
		case "Crazy Eights":
			crazyIdx = i
		}
	}
	require.NotEqual(t, -1, pokerIdx, "Poker must be offered")
	require.NotEqual(t, -1, crazyIdx, "Crazy Eights must be offered")

	m.gameIndex = pokerIdx
	m.maxPlayers = 9
	m.clampMaxPlayers()
	assert.Equal(t, 9, m.maxPlayers, "poker allows nine seats")

	m.gameIndex = crazyIdx
	m.clampMaxPlayers()
	assert.Equal(t, 6, m.maxPlayers, "crazy eights caps at six, so the setting must come down")
}

// The clamp must also raise a too-small setting to the game's minimum.
func TestCreate_ClampRaisesBelowMinimum(t *testing.T) {
	t.Parallel()
	m := newCreateModel(t)

	m.maxPlayers = 1
	m.clampMaxPlayers()
	assert.GreaterOrEqual(t, m.maxPlayers, 2, "no game starts with fewer than two seats")
}

// An in-range value must be left alone.
func TestCreate_ClampLeavesValidValueAlone(t *testing.T) {
	t.Parallel()
	m := newCreateModel(t)

	m.maxPlayers = 4
	m.clampMaxPlayers()
	assert.Equal(t, 4, m.maxPlayers)
}

// The form used to fall back to a hardcoded "Crazy Eights" when the registry came
// back empty, which made this a second place a game was declared and offered a
// game the registry could not build. An empty registry now offers nothing.
func TestCreate_OffersOnlyWhatTheRegistryHas(t *testing.T) {
	t.Parallel()

	global := router.GlobalContext{
		User:         testUser(1, "alice"),
		GameRegistry: game.NewRegistry(),
		Width:        120,
		Height:       40,
	}
	m, ok := NewCreate(global).(*createModel)
	require.True(t, ok)

	assert.Empty(t, m.gameOptions, "no registered game means no game on the form")
	assert.Empty(t, m.selectedGame())

	require.NotPanics(t, func() { m.View() }, "an empty form still has to render")

	m.cursor = createCursorSubmit
	_, cmd := m.handleKey(keyMsg("enter"))
	assert.Nil(t, cmd, "there is nothing to create, so nowhere to navigate")
	assert.ErrorIs(t, m.err, errNoGames)
}

// Every registered game must be on the form, in the registry's order.
func TestCreate_OffersEveryRegisteredGame(t *testing.T) {
	t.Parallel()
	m := newCreateModel(t)

	assert.ElementsMatch(t, m.global.GameRegistry.GameNames(), m.gameOptions)
}

func TestCreate_NavigationKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
		want string
	}{
		{name: "new game", key: "n", want: router.RouteLobbyCreate},
		{name: "join game", key: "f", want: router.RouteLobbyJoin},
		{name: "profile", key: "p", want: router.RouteProfile},
		{name: "leaderboard", key: "t", want: router.RouteLeaderboard},
		{name: "escape goes home", key: "esc", want: router.RouteHome},
		{name: "quit goes home", key: "q", want: router.RouteHome},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newCreateModel(t)
			_, cmd := m.Update(keyMsg(tt.key))
			assert.Equal(t, tt.want, routeOf(t, cmd))
		})
	}
}
