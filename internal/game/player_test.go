package game

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPlayer_Equal(t *testing.T) {
	t.Parallel()
	p1 := &Player{ID: "a", UserID: 1}
	p2 := &Player{ID: "b", UserID: 1}
	p3 := &Player{ID: "a", UserID: 2}

	t.Run("same account is the same player whatever the session ID", func(t *testing.T) {
		t.Parallel()
		assert.True(t, p1.Equal(p2))
	})

	t.Run("different accounts are different players", func(t *testing.T) {
		t.Parallel()
		assert.False(t, p1.Equal(p3))
	})

	t.Run("nil receiver", func(t *testing.T) {
		t.Parallel()
		var pNil *Player
		assert.False(t, pNil.Equal(p1))
	})

	t.Run("no account falls back to ID", func(t *testing.T) {
		t.Parallel()
		a := &Player{ID: "x"}
		b := &Player{ID: "x"}
		c := &Player{ID: "y"}
		assert.True(t, a.Equal(b))
		assert.False(t, a.Equal(c))
		assert.False(t, (&Player{}).Equal(&Player{}), "two unidentified players are not the same one")
	})
}

func TestPlayer_DisplayName(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "alice", (&Player{ID: "1", Name: "alice"}).DisplayName())
	assert.Equal(t, "1", (&Player{ID: "1"}).DisplayName(), "a nameless seat shows its ID")
	assert.Empty(t, (*Player)(nil).DisplayName())
}
