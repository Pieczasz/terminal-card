package ssh

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	// Wish runs a real bubbletea Program per session. Closing the SSH channel
	// cancels the program, but in-flight tea.Tick cmds can still be parked on a
	// timer when the package's TestMain verifies leaks — especially under -race
	// with two sessions (displace). Those are the library's timers, not ours.
	goleak.VerifyTestMain(m,
		goleak.IgnoreTopFunction("charm.land/bubbletea/v2.Tick.func1"),
		goleak.IgnoreTopFunction("charm.land/bubbletea/v2.(*Program).execBatchMsg"),
		goleak.IgnoreTopFunction("charm.land/bubbletea/v2.(*Program).execBatchMsg.func2"),
	)
}
