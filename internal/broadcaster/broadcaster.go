// Package broadcaster has implementation for subscribing and unsubscribing
// from broadcasting channel to receive live updates for lobby/game state.
package broadcaster

import (
	"log/slog"
	"sync"
)

const defaultMaxSubscribers = 64

type subscriber[T any] struct {
	id int
	ch chan T
}

// TODO: if the project grows and requires horizontal scaling across multiple
// servers, we should consider replacing this custom broadcaster with a
// pub/sub library like Watermill (using Redis or go channels).
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

func (b *Broadcaster[T]) Subscribe() <-chan T {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed || len(b.subscribers) >= b.maxSubscribers {
		ch := make(chan T)
		close(ch)
		return ch
	}

	// Using a larger buffer size to reduce the chance of dropped messages under heavy UI interaction
	ch := make(chan T, 256)
	b.subscribers[b.nextID] = &subscriber[T]{ch: ch, id: b.nextID}
	b.nextID++
	return ch
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

// Len returns the number of active subscribers (tests/metrics).
func (b *Broadcaster[T]) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}
