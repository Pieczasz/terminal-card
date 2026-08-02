package home

import (
	"testing"

	"github.com/Pieczasz/terminal-card/internal/tui/router"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHome_Update_Navigation(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Built per subtest: sharing one model across parallel subtests is safe
			// only while home.model keeps value receivers, and every other view in
			// this tree has since moved to pointer receivers.
			m := New(router.GlobalContext{})

			_, cmd := m.Update(tea.KeyPressMsg{Code: rune(tt.key[0]), Text: tt.key})

			require.NotNil(t, cmd, "%q must navigate", tt.key)
			msg, ok := cmd().(router.ChangeViewMsg)
			require.True(t, ok, "%q must produce a ChangeViewMsg", tt.key)
			assert.Equal(t, tt.want, msg.ViewName)
		})
	}
}

// q quits outright rather than navigating; it is the only bound key on this screen
// that does not produce a ChangeViewMsg.
func TestHome_Update_QuitsOnQ(t *testing.T) {
	t.Parallel()
	m := New(router.GlobalContext{})

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})

	require.NotNil(t, cmd)
	_, isChange := cmd().(router.ChangeViewMsg)
	assert.False(t, isChange, "q must not navigate")
}

func TestHome_Update_IgnoresUnboundKeys(t *testing.T) {
	t.Parallel()
	m := New(router.GlobalContext{})

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})

	assert.Nil(t, cmd, "an unbound key does nothing")
}
