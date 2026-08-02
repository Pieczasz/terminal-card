package styles_test

import (
	"fmt"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/tui/styles"

	"github.com/stretchr/testify/assert"
)

// Router.Global.Width/Height are zero until the first WindowSizeMsg, so every
// session renders its opening frame through these with a zero size. None of them may
// return a negative dimension.
func TestSizing_NeverNegative(t *testing.T) {
	t.Parallel()

	for _, size := range []int{0, 1, 2, 5, 10, 29, 30, 99, 100, 400} {
		t.Run(fmt.Sprintf("size=%d", size), func(t *testing.T) {
			t.Parallel()
			assert.GreaterOrEqual(t, styles.BoxWidth(size), 0, "BoxWidth")
			assert.GreaterOrEqual(t, styles.BoxHeight(size), 0, "BoxHeight")
			assert.GreaterOrEqual(t, styles.InnerWidth(size), 0, "InnerWidth")
			assert.GreaterOrEqual(t, styles.AvailableContentWidth(size), 0, "AvailableContentWidth")
			assert.GreaterOrEqual(t, styles.AvailableContentHeight(size, "hdr", "ftr"), 0, "AvailableContentHeight")
		})
	}
}

// A box must never be wider than the terminal that holds it.
func TestSizing_FitsWithinScreen(t *testing.T) {
	t.Parallel()

	for size := range 400 {
		assert.LessOrEqual(t, styles.BoxWidth(size), max(size, 0), "BoxWidth(%d) overflows the screen", size)
		assert.LessOrEqual(t, styles.BoxHeight(size), max(size, 0), "BoxHeight(%d) overflows the screen", size)
		assert.LessOrEqual(t, styles.InnerWidth(size), max(size, 0), "InnerWidth(%d) overflows the screen", size)
	}
}

// Growing the terminal must never shrink the box. A previous form switched to a 5/6
// proportion at 100 columns, so widening from 99 to 100 lost 12 columns.
func TestSizing_IsMonotonic(t *testing.T) {
	t.Parallel()

	for size := 1; size < 400; size++ {
		assert.GreaterOrEqual(t, styles.BoxWidth(size), styles.BoxWidth(size-1),
			"BoxWidth shrank going from %d to %d columns", size-1, size)
		assert.GreaterOrEqual(t, styles.BoxHeight(size), styles.BoxHeight(size-1),
			"BoxHeight shrank going from %d to %d rows", size-1, size)
		assert.GreaterOrEqual(t, styles.InnerWidth(size), styles.InnerWidth(size-1),
			"InnerWidth shrank going from %d to %d columns", size-1, size)
		assert.GreaterOrEqual(t, styles.AvailableContentWidth(size), styles.AvailableContentWidth(size-1),
			"AvailableContentWidth shrank going from %d to %d columns", size-1, size)
	}
}

// The box stops growing so text stays readable on an ultra-wide terminal.
func TestSizing_StopsGrowing(t *testing.T) {
	t.Parallel()

	assert.Equal(t, styles.BoxWidth(400), styles.BoxWidth(4000), "width is capped")
	assert.Equal(t, styles.BoxHeight(400), styles.BoxHeight(4000), "height is capped")
}
