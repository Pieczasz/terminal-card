package leaderboard

import (
	"fmt"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"

	"gorm.io/gorm"
)

// BenchmarkLeaderboardView_Render is the cost of one frame. The board renders on
// navigation rather than on a tick, so this is per visit, not per second.
func BenchmarkLeaderboardView_Render(b *testing.B) {
	rankings := make([]db.Ranking, 0, 25)
	for i := range 25 {
		rankings = append(rankings, db.Ranking{
			UserID: uint(i + 1),
			Elo:    uint32(2400 - i*30),
			User:   db.User{Model: gorm.Model{ID: uint(i + 1)}, Username: fmt.Sprintf("player%d", i+1)},
			Game:   db.Game{Name: "Poker"},
		})
	}
	m := model{
		global:   router.GlobalContext{Width: 120, Height: 40, Theme: styles.NewTheme(true)},
		rankings: rankings,
	}

	b.ReportAllocs()
	for b.Loop() {
		_ = m.View()
	}
}
