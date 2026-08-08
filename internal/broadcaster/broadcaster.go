// Package broadcaster has implementation for subscribing and unsubscribing
// from broadcasting channel to receive live updates for lobby/game state.
package broadcaster

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

const defaultMaxSubscribers = 64

// subscriberBuffer is how many events a slow subscriber may fall behind before
// Broadcast starts dropping its oldest.
const subscriberBuffer = 256

// Subscribe reports these rather than handing back a channel that is already
// closed. A closed channel is indistinguishable from a finished game, so a caller
// given one silently stops receiving events and has no way to tell why.
var (
	ErrClosed     = errors.New("broadcaster is closed")
	ErrAtCapacity = errors.New("broadcaster is at subscriber capacity")
)

type subscriber[T any] struct {
	id int
	ch chan T
}

// Broadcaster is built for the single-node deployment.
// Horizontal scaling across servers would need a shared pub/sub (e.g., Watermill
// over Redis); revisit only if the app runs multi-node.
type Broadcaster[T any] struct {
	mu             sync.RWMutex
	subscribers    map[int]*subscriber[T]
	nextID         int
	closed         bool
	maxSubscribers int
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
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}

	for _, sub := range b.subscribers {
		select {
		case sub.ch <- msg:
		default:
			// Buffer full: drop the oldest and enqueue the newest (latest-wins)
			// so slow subscribers don't get stuck on stale state.
			slog.Warn("broadcaster channel is full, dropping oldest message", "subscriberID", sub.id)
			select {
			case <-sub.ch:
			default:
			}
			select {
			case sub.ch <- msg:
			default:
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
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}
