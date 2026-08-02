package poker

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A chip stack has to add up to the stack it draws, or the table lies about who
// is winning. Change below the smallest chip is deliberately not drawn.
func TestRenderChipStack_BreaksAmountIntoWholeChips(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		amount uint
		want   string
	}{
		{name: "nothing to draw", amount: 0, want: ""},
		{name: "one of the smallest chip", amount: 10, want: "●1"},
		{name: "one of each", amount: 185, want: "●1 ●1 ●1 ●1"},
		{name: "hundreds only", amount: 1000, want: "●10"},
		{name: "change below the smallest chip is dropped", amount: 7, want: ""},
		{name: "a full stack with change", amount: 1287, want: "●12 ●1 ●1 ●1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, stripANSI(renderChipStack(tt.amount)))
		})
	}
}

func TestChipForKey(t *testing.T) {
	t.Parallel()

	value, ok := chipForKey("1")
	assert.True(t, ok)
	assert.Equal(t, uint(100), value, "key 1 is the largest chip")

	value, ok = chipForKey("4")
	assert.True(t, ok)
	assert.Equal(t, smallestChip(), value)

	_, ok = chipForKey("5")
	assert.False(t, ok, "there is no fifth chip")
}

var sgrPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// stripANSI drops styling so an assertion reads the chips, not the colour codes.
func stripANSI(s string) string {
	return sgrPattern.ReplaceAllString(s, "")
}
