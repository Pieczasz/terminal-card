package game

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
}
