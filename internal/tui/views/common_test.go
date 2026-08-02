package views_test

import (
	"strings"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"
	"github.com/Pieczasz/terminal-card/internal/tui/views"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The footer advertises a set of shortcuts and GlobalRoute resolves them. Nothing
// tied the two together, so a key could be shown to players and silently do nothing.
func TestGlobalActionsAllRoute(t *testing.T) {
	t.Parallel()

	for _, action := range styles.GlobalActions {
		key, _, found := strings.Cut(action, " - ")
		require.True(t, found, "footer entry %q must read '<key> - <label>'", action)
		key = strings.TrimSpace(key)

		t.Run(key, func(t *testing.T) {
			t.Parallel()
			if key == "ctrl+c" {
				// Quit is handled by HandleCommonMsg, not by the route table.
				handled, cmd := views.HandleCommonMsg(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, &router.GlobalContext{})
				assert.True(t, handled, "ctrl+c must be handled")
				assert.NotNil(t, cmd, "ctrl+c must produce the quit command")
				return
			}
			route, ok := views.GlobalRoute(key)
			assert.True(t, ok, "footer offers %q but no route resolves it", key)
			assert.NotEmpty(t, route)
		})
	}
}

func TestNavigateOn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		key     string
		want    string
		handled bool
	}{
		{name: "new lobby", key: "n", want: router.RouteLobbyCreate, handled: true},
		{name: "join lobby", key: "f", want: router.RouteLobbyJoin, handled: true},
		{name: "profile", key: "p", want: router.RouteProfile, handled: true},
		{name: "leaderboard", key: "t", want: router.RouteLeaderboard, handled: true},
		{name: "escape means back", key: "esc", want: router.RouteHome, handled: true},
		{name: "q means back", key: "q", want: router.RouteHome, handled: true},
		{name: "an unbound key is left to the view", key: "z", handled: false},
		{name: "enter is left to the view", key: "enter", handled: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd, ok := views.NavigateOn(tt.key)
			require.Equal(t, tt.handled, ok)
			if !tt.handled {
				assert.Nil(t, cmd, "an unhandled key must not produce a command")
				return
			}

			require.NotNil(t, cmd)
			msg, isChange := cmd().(router.ChangeViewMsg)
			require.True(t, isChange, "a handled key must navigate")
			assert.Equal(t, tt.want, msg.ViewName)
		})
	}
}

// esc and q are deliberately absent from the shared table: the lobby view uses esc
// for its leave confirmation, so only NavigateOn may treat them as "back".
func TestGlobalRouteExcludesBackKeys(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"esc", "q"} {
		_, ok := views.GlobalRoute(key)
		assert.False(t, ok, "%q must not be a global shortcut", key)
	}
}
