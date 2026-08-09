package crazyeight

import (
	"fmt"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/crazyeight"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// benchTable seats n players and returns the first player's view. Rendering is the
// per-frame cost every client pays, so it is the number that decides table capacity.
func benchTable(b *testing.B, n int) *Model {
	b.Helper()
	players := make([]*game.Player, 0, n)
	for i := range n {
		players = append(players, &game.Player{
			ID:     fmt.Sprint(i + 1),
			UserID: uint(i + 1), Name: fmt.Sprintf("p%d", i+1),
		})
	}
	engine := game.NewEngine(&logic.Rules{}, players, deck.StandardDeck())
	require.NoError(b, engine.Start())
	b.Cleanup(engine.Close)

	global := router.GlobalContext{
		User:  &db.User{Model: gorm.Model{ID: 1}, Username: "p1"},
		Width: 120, Height: 40, Theme: styles.NewTheme(true),
	}
	m, ok := New(global, engine).(*Model)
	require.True(b, ok)
	b.Cleanup(m.Close)
	return m
}

func BenchmarkCrazyEightView_Render(b *testing.B) {
	for _, seats := range []int{2, 4} {
		b.Run(fmt.Sprintf("seats=%d", seats), func(b *testing.B) {
			m := benchTable(b, seats)
			b.ReportAllocs()
			for b.Loop() {
				_ = m.View()
			}
		})
	}
}
