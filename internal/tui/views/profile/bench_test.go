package profile

import (
	"fmt"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"

	"gorm.io/gorm"
)

func BenchmarkProfileView_Render(b *testing.B) {
	user := &db.User{Model: gorm.Model{ID: 1}, Username: "alice"}
	for i := range 3 {
		user.Rankings = append(user.Rankings, db.Ranking{
			Elo: uint32(1500 + i*40), Game: db.Game{Name: fmt.Sprintf("Game%d", i)},
		})
	}
	history := make([]db.MatchParticipant, 0, 10)
	for i := range 10 {
		history = append(history, db.MatchParticipant{
			Placement: i%3 + 1, EloDelta: 12 - i,
			Match: db.Match{Ranked: i%2 == 0, Game: db.Game{Name: "Poker"}},
		})
	}
	m := model{
		global:      router.GlobalContext{User: user, Width: 120, Height: 40, Theme: styles.NewTheme(true)},
		userProfile: user,
		history:     history,
	}

	b.ReportAllocs()
	for b.Loop() {
		_ = m.View()
	}
}
