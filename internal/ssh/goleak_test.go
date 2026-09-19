package ssh

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	// Wish runs a real bubbletea Program per session. Closing the SSH channel
	// cancels the program, but in-flight Batch/Tick work can still sit in
	// WaitGroup.Wait when TestMain verifies leaks - especially under -race with
	// two sessions (displace). Top of stack is sync.runtime_SemacquireWaitGroup and
	// the only repo frames are below bubbletea's, so these have to match any frame:
	// IgnoreTopFunction here fails with bubbletea's own execBatchMsg parked.
	//
	// Program.eventLoop is deliberately NOT ignored. It runs synchronously inside
	// Program.Run, so a goroutine still in it means Run never returned - the session
	// never tore down, its tracker slot and lobby seat still held. That is the leak
	// this package exists to catch, and the suite passes without ignoring it.
	goleak.VerifyTestMain(m,
		goleak.IgnoreAnyFunction("charm.land/bubbletea/v2.Tick.func1"),
		goleak.IgnoreAnyFunction("charm.land/bubbletea/v2.(*Program).execBatchMsg"),
		goleak.IgnoreAnyFunction("charm.land/bubbletea/v2.(*Program).execBatchMsg.func2"),
	)
}
