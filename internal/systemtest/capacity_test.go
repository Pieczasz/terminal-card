package systemtest

import (
	"context"
	"fmt"
	"runtime"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/lobby"
	"github.com/Pieczasz/terminal-card/internal/player"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// heapInUse settles the heap and reports what is live, so a before/after pair
// measures retained memory rather than allocation churn.
func heapInUse() uint64 {
	for range 3 {
		runtime.GC()
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapAlloc
}

// TestCapacity_MemoryPerTable reports the retained cost of a live table: the lobby,
// its broadcaster, the engine, the deck and every seat. It is a measurement, not an
// assertion about a threshold - the bound is generous purely so a runaway regression
// fails rather than being quietly reported.
//
//nolint:paralleltest // measures process-wide heap, so it cannot share the process
func TestCapacity_MemoryPerTable(t *testing.T) {
	const tables = 200
	const seatsPerTable = 6

	manager := lobby.NewManager(context.Background(), nil)
	registry := realRegistry(t)

	// Build everything first so the measurement excludes one-off package state.
	warm := openTable(t, manager, registry, 0, seatsPerTable)
	require.NotNil(t, warm)

	before := heapInUse()
	held := make([]*lobby.Lobby, 0, tables)
	for i := 1; i <= tables; i++ {
		held = append(held, openTable(t, manager, registry, i, seatsPerTable))
	}
	after := heapInUse()
	runtime.KeepAlive(held)

	perTable := float64(after-before) / float64(tables)
	t.Logf("MEASUREMENT retained per %d-seat table in play: %.1f KiB (%.1f KiB per seat)",
		seatsPerTable, perTable/1024, perTable/float64(seatsPerTable)/1024)
	t.Logf("MEASUREMENT %d tables held %.1f MiB of heap", tables, float64(after-before)/(1<<20))

	require.Less(t, perTable, float64(2<<20), "a table must not retain megabytes")
}

// openTable seats n players in a fresh lobby and starts the game, which is the state
// a table spends essentially all of its life in.
func openTable(t *testing.T, manager *lobby.Manager, registry *game.Registry, idx, n int) *lobby.Lobby {
	t.Helper()
	leader := benchPlayer(idx, 0)
	l, err := manager.New(leader,
		lobby.WithCardGame(&db.Game{Name: "Poker"}),
		lobby.WithMaxPlayers(9),
		lobby.WithPrivate(false),
	)
	require.NoError(t, err)

	guests := make([]*player.Player, 0, n-1)
	for i := 1; i < n; i++ {
		g := benchPlayer(idx, i)
		require.NoError(t, manager.JoinLobbyByCode(l.Code(), g))
		guests = append(guests, g)
	}

	require.NoError(t, l.ToggleReady(leader, registry))
	for _, g := range guests {
		require.NoError(t, l.ToggleReady(g, registry))
	}
	return l
}

func benchPlayer(table, seat int) *player.Player {
	id := fmt.Sprintf("t%d-s%d", table, seat)
	return &player.Player{
		ID: id,
		DatabaseUser: &db.User{
			Model:    gorm.Model{ID: uint(table*100 + seat + 1)},
			Username: id,
		},
	}
}
