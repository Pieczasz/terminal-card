package player

import (
	"testing"

	"github.com/Pieczasz/terminal-card/internal/db"

	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestPlayer_Compare(t *testing.T) {
	p1 := &Player{DatabaseUser: &db.User{Model: gorm.Model{ID: 1}}}
	p2 := &Player{DatabaseUser: &db.User{Model: gorm.Model{ID: 1}}}
	p3 := &Player{DatabaseUser: &db.User{Model: gorm.Model{ID: 2}}}

	t.Run("identical IDs", func(t *testing.T) {
		got := p1.Equal(p2)
		assert.True(t, got)
	})

	t.Run("different IDs", func(t *testing.T) {
		got := p1.Equal(p3)
		assert.False(t, got)
	})

	t.Run("nil receiver", func(t *testing.T) {
		var pNil *Player
		got := pNil.Equal(p1)
		assert.False(t, got)
	})

	t.Run("nil database user falls back to ID", func(t *testing.T) {
		a := &Player{ID: "x"}
		b := &Player{ID: "x"}
		c := &Player{ID: "y"}
		assert.True(t, a.Equal(b))
		assert.False(t, a.Equal(c))
	})
}
