package lobby

import (
	"context"
	"fmt"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/db"

	"github.com/stretchr/testify/require"
)

func TestLobby_EveryPlayerGetsAFeedAfterTheTableGrows(t *testing.T) {
	t.Parallel()
	m := NewManager(context.Background(), nil)
	leader := mockPlayer("leader", 1)
	l, err := m.New(leader, WithMaxPlayers(2), WithCardGame(&db.Game{Name: "Uno"}))
	require.NoError(t, err)

	require.NoError(t, l.SetMaxPlayers(leader, 10, 2, 10))

	ids := []string{leader.ID}
	for i := 1; i < 10; i++ {
		guest := mockPlayer(fmt.Sprintf("guest%d", i), uint(i+1))
		require.NoError(t, m.JoinLobbyByCode(l.Code(), guest))
		ids = append(ids, guest.ID)
	}
	require.Equal(t, 10, l.CurrentPlayers())

	for _, id := range ids {
		_, err := l.Subscribe(id)
		require.NoErrorf(t, err, "seat %s was refused a lobby feed", id)
	}

	_, err = l.Subscribe(leader.ID)
	require.NoError(t, err, "a reconnecting player must not be refused")
}
