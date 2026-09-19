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

// A half-declared module is a wiring bug, and a registry that accepted one would fail
// later as a missing route or a nil factory panic at the moment somebody starts a
// table. catalog_test.go leans on this being loud.
func TestRegistry_RegisterModuleRejectsAHalfDeclaredGame(t *testing.T) {
	t.Parallel()

	factory := func() Rules { return &MockRules{} }
	tests := []struct {
		name   string
		module Module
	}{
		{name: "no display name", module: Module{Slug: "s", Factory: factory}},
		{name: "no slug", module: Module{Name: "N", Factory: factory}},
		{name: "no factory", module: Module{Name: "N", Slug: "s"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Panics(t, func() { NewRegistry().RegisterModule(tt.module) })
		})
	}
}

// Re-registering a name replaces the module without listing it twice: GameNames drives
// the menu, and a duplicate row is a game the player can pick and never reach.
func TestRegistry_ReRegisterKeepsOneEntry(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.RegisterModule(Module{Name: "Poker", Slug: "old", Factory: func() Rules { return &MockRules{} }})
	r.RegisterModule(Module{Name: "Poker", Slug: "new", Factory: func() Rules { return &MockRules{} }})

	assert.Equal(t, []string{"Poker"}, r.GameNames())
	mod, ok := r.Module("Poker")
	require.True(t, ok)
	assert.Equal(t, "new", mod.Slug)
}
