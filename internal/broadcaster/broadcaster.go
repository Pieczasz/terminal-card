// Package broadcaster has implementation for subscribing and unsubscribing
// from broadcasting channel to receive live updates for lobby/game state.
package broadcaster

import "sync"

type subscriber[T any] struct {
	id int
	ch chan T
}

type Broadcaster[T any] struct {
	mu          sync.RWMutex
	subscribers map[int]*subscriber[T]
	nextId      int
}

func New[T any](maxSubscribers int) *Broadcaster[T] {
	return &Broadcaster[T]{
		subscribers: make(map[int]*subscriber[T], maxSubscribers),
	}
}

func (b *Broadcaster[T]) Subscribe() <-chan T {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan T, 10)
	b.subscribers[b.nextId] = &subscriber[T]{ch: ch, id: b.nextId}
	b.nextId++
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

	for _, sub := range b.subscribers {
		select {
		case sub.ch <- msg:
		default:
			// Channel is full. We drop the message rather than blocking the broadcaster.
			// TODO: check if there is something else we can do.
		}
	}
}

func (b *Broadcaster[T]) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for id, sub := range b.subscribers {
		close(sub.ch)
		delete(b.subscribers, id)
	}
}
