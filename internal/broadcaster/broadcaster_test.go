package broadcaster

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBroadcaster_Subscribe(t *testing.T) {
	t.Parallel()
	b := New[string](10)

	ch := mustSubscribe(t, b)
	assert.NotNil(t, ch)

	ch2 := mustSubscribe(t, b)
	assert.NotNil(t, ch2)

	b.mu.Lock()
	assert.Len(t, b.subscribers, 2)
	b.mu.Unlock()
}

func TestBroadcaster_Broadcast(t *testing.T) {
	t.Parallel()
	b := New[int](10)

	ch1 := mustSubscribe(t, b)
	ch2 := mustSubscribe(t, b)

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

	ch1 := mustSubscribe(t, b)
	ch2 := mustSubscribe(t, b)

	b.mu.Lock()
	assert.Len(t, b.subscribers, 2)
	b.mu.Unlock()

	b.Unsubscribe(ch1)

	b.mu.Lock()
	assert.Len(t, b.subscribers, 1)
	b.mu.Unlock()

	select {
	case _, ok := <-ch1:
		assert.False(t, ok, "the unsubscribed channel must be closed")
	case <-time.After(time.Second):
		t.Fatal("Unsubscribe left ch1 open, so it closed the wrong subscriber")
	}

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

	ch, err := b.Subscribe()
	require.ErrorIs(t, err, ErrClosed, "a closed broadcaster must say so, not hand back a dead channel")
	assert.Nil(t, ch)
	b.Close()
}

func TestBroadcaster_Close(t *testing.T) {
	t.Parallel()
	b := New[int](10)

	ch1 := mustSubscribe(t, b)
	ch2 := mustSubscribe(t, b)

	b.Close()

	b.mu.Lock()
	assert.Empty(t, b.subscribers)
	b.mu.Unlock()

	_, ok1 := <-ch1
	assert.False(t, ok1)

	_, ok2 := <-ch2
	assert.False(t, ok2)
}

func TestBroadcaster_NonBlockingFullChannel(t *testing.T) {
	t.Parallel()
	b := New[int](1)

	ch := mustSubscribe(t, b)

	for i := range 256 {
		b.Broadcast(i)
	}

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

	ch := mustSubscribe(t, b)

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

	for range broadcasters {
		wg.Go(func() {
			for i := range iterations {
				b.Broadcast(i)
			}
		})
	}

	for range subscribers {
		wg.Go(func() {
			for range iterations {
				ch, err := b.Subscribe()
				if err != nil {
					continue
				}
				select {
				case <-ch:
				default:
				}
				b.Unsubscribe(ch)
			}
		})
	}

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
	ch1 := mustSubscribe(t, b)
	ch2 := mustSubscribe(t, b)
	ch3, err := b.Subscribe()

	assert.Equal(t, 2, b.Len())
	require.ErrorIs(t, err, ErrAtCapacity, "over-capacity subscribe must report why")
	assert.Nil(t, ch3)

	b.Unsubscribe(ch1)
	ch4 := mustSubscribe(t, b)
	assert.Equal(t, 2, b.Len())
	select {
	case <-ch4:
		t.Fatal("new subscriber channel should stay open")
	default:
	}
	_ = ch2
}

func TestBroadcaster_NonPositiveCapacityGetsTheDefault(t *testing.T) {
	t.Parallel()

	for _, capacity := range []int{0, -1} {
		b := New[int](capacity)
		t.Cleanup(b.Close)

		_, err := b.Subscribe()
		require.NoErrorf(t, err, "New(%d) must fall back to a usable capacity", capacity)
		assert.Equal(t, defaultMaxSubscribers, b.maxSubscribers)
	}
}

func BenchmarkBroadcast(b *testing.B) {
	for _, subs := range []int{1, 4, 9} {
		b.Run(fmt.Sprintf("subscribers=%d", subs), func(b *testing.B) {
			bc := New[int](subs + 8)
			defer bc.Close()

			done := make(chan struct{})
			var wg sync.WaitGroup
			for range subs {
				ch, err := bc.Subscribe()
				if err != nil {
					b.Fatal(err)
				}
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

func mustSubscribe[T any](t *testing.T, b *Broadcaster[T]) <-chan T {
	t.Helper()
	ch, err := b.Subscribe()
	require.NoError(t, err)
	return ch
}
