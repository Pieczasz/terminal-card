package ssh

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	// Wish runs a real bubbletea Program per session. Closing the SSH channel
	// cancels the program, but in-flight Batch/Tick work can still sit in
	// WaitGroup.Wait when TestMain verifies leaks — especially under -race with
	// two sessions (displace). Top of stack is often sync.runtime_Semacquire*,
	// so match any frame in the bubbletea package, not only Tick.func1.
	goleak.VerifyTestMain(m,
		goleak.IgnoreAnyFunction("charm.land/bubbletea/v2.Tick.func1"),
		goleak.IgnoreAnyFunction("charm.land/bubbletea/v2.(*Program).execBatchMsg"),
		goleak.IgnoreAnyFunction("charm.land/bubbletea/v2.(*Program).execBatchMsg.func2"),
		goleak.IgnoreAnyFunction("charm.land/bubbletea/v2.(*Program).eventLoop"),
	)
}
