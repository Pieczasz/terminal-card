package broadcaster

import (
	"fmt"
	"sync"
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

	for i := range 256 {
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

	// Latest-wins: the newest message must still be observable even though the
	// buffer was full, so the oldest value (0) has been evicted instead.
	var last int
	for {
		select {
		case v := <-ch:
			last = v
			continue
		default:
		}
		break
	}
	assert.Equal(t, 1001, last, "most recent broadcast should survive a full buffer")
}

func TestBroadcaster_LatestWins(t *testing.T) {
	t.Parallel()
	b := New[int](10)

	ch := b.Subscribe()

	// Overfill the subscriber buffer (256) without ever draining it. Under
	// latest-wins the oldest messages get evicted, but the final value must
	// remain enqueued.
	const total = 512
	for i := range total {
		b.Broadcast(i)
	}

	var values []int
	for {
		select {
		case v := <-ch:
			values = append(values, v)
			continue
		default:
		}
		break
	}

	if assert.NotEmpty(t, values) {
		assert.Equal(t, total-1, values[len(values)-1], "latest message must survive")
		assert.NotContains(t, values, 0, "oldest message should have been dropped")
	}
}

func TestBroadcaster_ConcurrentStress(t *testing.T) {
	t.Parallel()
	b := New[int](32)

	const (
		broadcasters = 8
		subscribers  = 8
		iterations   = 500
	)

	var wg sync.WaitGroup

	// Broadcasters hammer the broadcaster concurrently.
	for range broadcasters {
		wg.Go(func() {
			for i := range iterations {
				b.Broadcast(i)
			}
		})
	}

	// Subscribers repeatedly subscribe, drain, and unsubscribe.
	for range subscribers {
		wg.Go(func() {
			for range iterations {
				ch := b.Subscribe()
				select {
				case <-ch:
				default:
				}
				b.Unsubscribe(ch)
			}
		})
	}

	// Close concurrently once broadcasts are in flight; the broadcaster must
	// stay panic- and deadlock-free.
	go func() {
		time.Sleep(time.Millisecond)
		b.Close()
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent stress test deadlocked")
	}

	assert.Equal(t, 0, b.Len(), "all subscribers should be gone after Close")
}

func TestBroadcaster_MaxSubscribers(t *testing.T) {
	t.Parallel()
	b := New[int](2)
	ch1 := b.Subscribe()
	ch2 := b.Subscribe()
	ch3 := b.Subscribe()

	assert.Equal(t, 2, b.Len())
	_, ok := <-ch3
	assert.False(t, ok, "over-capacity subscribe should return closed channel")

	b.Unsubscribe(ch1)
	ch4 := b.Subscribe()
	assert.Equal(t, 2, b.Len())
	select {
	case <-ch4:
		t.Fatal("new subscriber channel should stay open")
	default:
	}
	_ = ch2
}

// Broadcast is on the per-event fan-out path: every engine action reaches every
// subscribed player, so its cost scales with table size.
func BenchmarkBroadcast(b *testing.B) {
	for _, subs := range []int{1, 4, 9} {
		b.Run(fmt.Sprintf("subscribers=%d", subs), func(b *testing.B) {
			bc := New[int](subs + 8)
			defer bc.Close()

			// Drain in the background so Broadcast measures fan-out, not queue-full
			// drop handling.
			done := make(chan struct{})
			var wg sync.WaitGroup
			for range subs {
				ch := bc.Subscribe()
				wg.Go(func() {
					for {
						select {
						case <-ch:
						case <-done:
							return
						}
					}
				})
			}

			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				bc.Broadcast(i)
			}
			b.StopTimer()
			close(done)
			wg.Wait()
		})
	}
}
