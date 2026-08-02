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
	require.Error(t, err)

	r.RegisterModule(Module{
		Name:    "FakeGame",
		Slug:    "fakegame",
		Factory: func() Rules { return &MockRules{} },
	})

	names := r.GameNames()
	assert.Len(t, names, 1)
	assert.Equal(t, "FakeGame", names[0])

	rules, err := r.Create("FakeGame")
	require.NoError(t, err)
	assert.NotNil(t, rules)

	mod, ok := r.Module("FakeGame")
	require.True(t, ok)
	assert.Equal(t, "fakegame", mod.Slug)

	_, ok = r.Module("NotExists")
	assert.False(t, ok)
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

	mod, ok := r.Module("Crazy Eights")
	require.True(t, ok)
	assert.Equal(t, "crazy_eights", mod.Slug)
}
