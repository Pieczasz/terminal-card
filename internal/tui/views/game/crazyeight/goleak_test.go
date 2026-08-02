package crazyeight

import (
	"testing"

	"go.uber.org/goleak"
)

// These tests subscribe to broadcasters and start listener goroutines; a Close or
// Unsubscribe regression parks one forever, which no assertion would notice.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
