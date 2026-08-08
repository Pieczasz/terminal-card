package home

import (
	"testing"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"

	"gorm.io/gorm"
)

func BenchmarkHomeView_Render(b *testing.B) {
	m := model{global: router.GlobalContext{
		User:  &db.User{Model: gorm.Model{ID: 1}, Username: "alice"},
		Width: 120, Height: 40, Theme: styles.NewTheme(true),
	}}

	b.ReportAllocs()
	for b.Loop() {
		_ = m.View()
	}
}
