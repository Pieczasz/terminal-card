// Package broadcaster has implementation for subscribing and unsubscribing
// from broadcasting channel to receive live updates for lobby/game state.
package broadcaster

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
)

const defaultMaxSubscribers = 64
const subscriberBuffer = 256

var (
	ErrClosed     = errors.New("broadcaster is closed")
	ErrAtCapacity = errors.New("broadcaster is at subscriber capacity")
)

type subscriber[T any] struct {
	id      int
	ch      chan T
	dropped int
}

// Broadcaster is built for the single-node deployment.
// Horizontal scaling across servers would need a shared pub/sub (e.g., Watermill
// over Redis); revisit only if the app runs multi-node.
type Broadcaster[T any] struct {
	// Broadcast takes the write lock: it may drain a subscriber's channel on a full
	// buffer, and two concurrent broadcasts draining the same channel would steal
	// messages from each other and from the subscriber. Exclusive sends also make
	// Unsubscribe's close-from-the-receiver-side safe by construction.
	mu             sync.Mutex
	subscribers    map[int]*subscriber[T]
	nextID         int
	closed         bool
	maxSubscribers int
	dropped        atomic.Int64
}

func New[T any](maxSubscribers int) *Broadcaster[T] {
	if maxSubscribers <= 0 {
		maxSubscribers = defaultMaxSubscribers
	}
	return &Broadcaster[T]{
		subscribers:    make(map[int]*subscriber[T], maxSubscribers),
		maxSubscribers: maxSubscribers,
	}
}

func (b *Broadcaster[T]) Subscribe() (<-chan T, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil, ErrClosed
	}
	if len(b.subscribers) >= b.maxSubscribers {
		return nil, fmt.Errorf("%w of %d", ErrAtCapacity, b.maxSubscribers)
	}

	ch := make(chan T, subscriberBuffer)
	b.subscribers[b.nextID] = &subscriber[T]{ch: ch, id: b.nextID}
	b.nextID++
	return ch, nil
}

func (b *Broadcaster[T]) Unsubscribe(ch <-chan T) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for id, subscriber := range b.subscribers {
		if subscriber.ch == ch {
			delete(b.subscribers, id)
			close(subscriber.ch)
			return
		}
	}
}

func (b *Broadcaster[T]) Broadcast(msg T) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	for _, sub := range b.subscribers {
		select {
		case sub.ch <- msg:
		default:
			// Buffer full: drop the oldest and enqueue the newest (latest-wins)
			// so slow subscribers don't get stuck on stale state. Logged once per
			// subscriber and counted, not logged per message - a stuck subscriber
			// would otherwise turn every broadcast into a log write under the lock.
			b.dropped.Add(1)
			sub.dropped++
			if sub.dropped == 1 {
				slog.Warn("broadcaster channel is full, dropping oldest messages",
					"subscriber_id", sub.id)
			}
			select {
			case <-sub.ch:
			default:
			}
			select {
			case sub.ch <- msg:
			default:
				b.dropped.Add(1)
				sub.dropped++
			}
		}
	}
}

func (b *Broadcaster[T]) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true

	for id, sub := range b.subscribers {
		close(sub.ch)
		delete(b.subscribers, id)
	}
}

func (b *Broadcaster[T]) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subscribers)
}

// Dropped is the total number of messages discarded across all subscribers since
// construction. Owners surface it as a metric; a rising count means a subscriber
// is not keeping up.
func (b *Broadcaster[T]) Dropped() int64 {
	return b.dropped.Load()
}
