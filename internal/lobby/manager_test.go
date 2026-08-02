package lobby

import (
	"context"
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/player"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestManager_WaitForFinalizers(t *testing.T) {
	t.Parallel()
	m := NewManager(context.Background(), nil)

	assert.True(t, m.WaitForFinalizers(time.Second), "nothing in flight drains immediately")

	m.finalizing.Add(1)
	assert.False(t, m.WaitForFinalizers(50*time.Millisecond), "an in-flight write blocks the drain")

	m.finalizing.Done()
	assert.True(t, m.WaitForFinalizers(time.Second), "drains once the write completes")
}

// Shutdown may run before a manager was ever built.
func TestManager_WaitForFinalizers_NilReceiver(t *testing.T) {
	t.Parallel()
	var m *Manager
	assert.True(t, m.WaitForFinalizers(time.Second))
}

// FuzzJoinLobbyByCode covers a trust boundary: the code is typed by a remote SSH
// client, so arbitrary bytes reach the lookup. Nothing may panic, and no input may
// smuggle a player into a lobby they were not invited to.
func FuzzJoinLobbyByCode(f *testing.F) {
	f.Add("")
	f.Add("ABCD1234")
	f.Add("abcd1234")
	f.Add("../../etc/passwd")
	f.Add("' OR 1=1 --")
	f.Add("ABCD123\x00")

	f.Fuzz(func(t *testing.T, code string) {
		m := NewManager(context.Background(), nil)
		host := &player.Player{ID: "host", DatabaseUser: &db.User{Model: gorm.Model{ID: 1}}}
		l, err := m.New(host, WithCardGame(&db.Game{Name: "Poker"}), WithMaxPlayers(4))
		require.NoError(t, err)

		joiner := &player.Player{ID: "joiner", DatabaseUser: &db.User{Model: gorm.Model{ID: 2}}}
		err = m.JoinLobbyByCode(code, joiner)

		if code == l.Code() {
			require.NoError(t, err, "the real code must work")
			assert.True(t, l.HasPlayer(joiner))
			return
		}
		require.Error(t, err, "only the issued code may join")
		assert.False(t, l.HasPlayer(joiner), "a rejected code must not add the player")
		assert.Nil(t, m.FindLobbyByPlayer(joiner), "a rejected join leaves no membership")
	})
}
