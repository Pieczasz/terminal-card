package poker

import (
	"testing"

	"go.uber.org/goleak"
)

// The tests here drive real engines, which arm a turn timer on every cursor move.
// A leaked timer goroutine means a table that was never closed.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
