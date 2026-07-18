package broadcaster

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBroadcaster_Subscribe(t *testing.T) {
	t.Parallel()
	b := New[string](10)

	ch := b.Subscribe()
	assert.NotNil(t, ch)

	ch2 := b.Subscribe()
	assert.NotNil(t, ch2)

	b.mu.RLock()
	assert.Len(t, b.subscribers, 2)
	b.mu.RUnlock()
}

func TestBroadcaster_Broadcast(t *testing.T) {
	t.Parallel()
	b := New[int](10)

	ch1 := b.Subscribe()
	ch2 := b.Subscribe()

	b.Broadcast(42)

	select {
	case got1 := <-ch1:
		assert.Equal(t, 42, got1)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ch1 did not receive message")
	}

	select {
	case got2 := <-ch2:
		assert.Equal(t, 42, got2)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ch2 did not receive message")
	}
}

func TestBroadcaster_Unsubscribe(t *testing.T) {
	t.Parallel()
	b := New[string](10)

	ch1 := b.Subscribe()
	ch2 := b.Subscribe()

	b.mu.RLock()
	assert.Len(t, b.subscribers, 2)
	b.mu.RUnlock()

	b.Unsubscribe(ch1)

	b.mu.RLock()
	assert.Len(t, b.subscribers, 1)
	b.mu.RUnlock()

	_, ok := <-ch1
	assert.False(t, ok, "channel should be closed")

	b.Broadcast("hello")

	select {
	case got2 := <-ch2:
		assert.Equal(t, "hello", got2)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ch2 did not receive message")
	}
}

func TestBroadcaster_SubscribeAfterClose(t *testing.T) {
	t.Parallel()
	b := New[int](10)
	b.Close()

	ch := b.Subscribe()
	_, ok := <-ch
	assert.False(t, ok)

	// Double close is safe.
	b.Close()
}

func TestBroadcaster_Close(t *testing.T) {
	t.Parallel()
	b := New[int](10)

	ch1 := b.Subscribe()
	ch2 := b.Subscribe()

	b.Close()

	b.mu.RLock()
	assert.Empty(t, b.subscribers)
	b.mu.RUnlock()

	_, ok1 := <-ch1
	assert.False(t, ok1)

	_, ok2 := <-ch2
	assert.False(t, ok2)
}

func TestBroadcaster_NonBlockingFullChannel(t *testing.T) {
	t.Parallel()
	b := New[int](1)

	ch := b.Subscribe()

	// The channel size is 10000, so broadcast 10000 times
	for i := range 10000 {
		b.Broadcast(i)
	}

	// This should not block even though the channel is full
	done := make(chan bool)
	go func() {
		b.Broadcast(1001)
		done <- true
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Broadcast blocked on a full channel")
	}

	// Read one to verify the first ones were retained (channels are FIFO)
	got := <-ch
	assert.Equal(t, 0, got)
}
