package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAll_EntriesComplete(t *testing.T) {
	t.Parallel()
	require.NotEmpty(t, All)

	names := make(map[string]bool, len(All))
	slugs := make(map[string]bool, len(All))
	for _, e := range All {
		require.NotEmpty(t, e.Name, "entry with slug %q has no name", e.Slug)
		require.NotEmpty(t, e.Slug, "entry %q has no slug", e.Name)
		require.NotNil(t, e.Rules, "entry %q has no rules factory", e.Name)
		require.NotNil(t, e.View, "entry %q has no TUI view", e.Name)

		assert.False(t, names[e.Name], "duplicate name %q", e.Name)
		assert.False(t, slugs[e.Slug], "duplicate slug %q", e.Slug)
		names[e.Name] = true
		slugs[e.Slug] = true

		assert.NotNil(t, e.Rules(), "rules factory for %q returned nil", e.Name)
	}
}

func TestEntry_Module(t *testing.T) {
	t.Parallel()
	for _, e := range All {
		m := e.Module()
		assert.Equal(t, e.Name, m.Name)
		assert.Equal(t, e.Slug, m.Slug)
		require.NotNil(t, m.Factory)
		assert.NotNil(t, m.Factory())
	}
}
