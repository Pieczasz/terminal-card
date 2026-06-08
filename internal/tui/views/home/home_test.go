package home

import (
	"testing"

	"terminalcard/internal/tui/router"

	tea "github.com/charmbracelet/bubbletea"
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
			keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)}
			_, cmd := m.Update(keyMsg)

			assert.NotNil(t, cmd)
			msg := cmd()

			changeMsg, ok := msg.(router.ChangeViewMsg)
			assert.True(t, ok)
			assert.Equal(t, tt.expectedView, changeMsg.ViewName)
		})
	}
}
