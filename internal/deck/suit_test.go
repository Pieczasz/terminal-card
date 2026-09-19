package deck

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A player names a suit for an Eight or a Wild, so the zero value, the sentinel and
// anything a client could invent must all be refused.
func TestIsSuit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		suit Suit
		want bool
	}{
		{"spades", Spades, true}, {"hearts", Hearts, true},
		{"diamonds", Diamonds, true}, {"clubs", Clubs, true},
		{"the NoSuit sentinel", NoSuit, false},
		{"a value past the last suit", Clubs + 1, false},
		{"garbage", Suit(200), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IsSuit(tc.suit))
		})
	}
}
