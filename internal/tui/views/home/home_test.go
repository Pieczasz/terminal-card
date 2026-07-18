package home

import (
	"testing"

	"github.com/Pieczasz/terminal-card/internal/tui/router"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
)

func TestHome_Update(t *testing.T) {
	t.Parallel()
	m := New(router.GlobalContext{})

	tests := []struct {
		key          string
		expectedView string
	}{
		{"n", "lobby_create"},
		{"f", "lobby_join"},
		{"p", "profile"},
	}

	for _, tt := range tests {
		t.Run("key_"+tt.key, func(t *testing.T) {
			keyMsg := tea.KeyPressMsg{Code: rune(tt.key[0]), Text: tt.key}
			_, cmd := m.Update(keyMsg)

			assert.NotNil(t, cmd)
			msg := cmd()

			changeMsg, ok := msg.(router.ChangeViewMsg)
			assert.True(t, ok)
			assert.Equal(t, tt.expectedView, changeMsg.ViewName)
		})
	}
}
