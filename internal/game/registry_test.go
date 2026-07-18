package game

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry(t *testing.T) {
	t.Parallel()
	r := NewRegistry()

	_, err := r.Create("NotExists")
	assert.Error(t, err)

	r.Register("FakeGame", func() Rules {
		return &MockRules{}
	})

	names := r.GameNames()
	assert.Len(t, names, 1)
	assert.Equal(t, "FakeGame", names[0])

	rules, err := r.Create("FakeGame")
	assert.NoError(t, err)
	assert.NotNil(t, rules)

	route, err := r.RouteName("FakeGame")
	require.NoError(t, err)
	assert.Equal(t, "game_fakegame", route)
}

func TestRegistry_RegisterModule(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.RegisterModule(Module{
		Name:    "Crazy Eights",
		Slug:    "crazy_eights",
		Factory: func() Rules { return &MockRules{} },
	})

	assert.Equal(t, []string{"Crazy Eights"}, r.GameNames())
	route, err := r.RouteName("Crazy Eights")
	require.NoError(t, err)
	assert.Equal(t, "game_crazy_eights", route)

	mod, ok := r.Module("Crazy Eights")
	require.True(t, ok)
	assert.Equal(t, "crazy_eights", mod.Slug)
}

func TestSlugify(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want string
	}{
		{"Crazy Eights", "crazy_eights"},
		{"Poker", "poker"},
		{"Texas Hold'em", "texas_holdem"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, slugify(tt.name))
		})
	}
}
