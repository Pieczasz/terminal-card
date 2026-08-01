package lobby

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestManager_WaitForFinalizers(t *testing.T) {
	t.Parallel()
	m := NewManagerWithContext(context.Background(), nil)

	assert.True(t, m.WaitForFinalizers(time.Second), "nothing in flight drains immediately")

	m.finalizing.Add(1)
	assert.False(t, m.WaitForFinalizers(50*time.Millisecond), "an in-flight write blocks the drain")

	m.finalizing.Done()
	assert.True(t, m.WaitForFinalizers(time.Second), "drains once the write completes")
}

// Shutdown may run before a manager was ever built.
func TestManager_WaitForFinalizers_NilReceiver(t *testing.T) {
	t.Parallel()
	var m *Manager
	assert.True(t, m.WaitForFinalizers(time.Second))
}
