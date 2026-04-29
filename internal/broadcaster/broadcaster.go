// Package broadcaster has implementation for subscribing and unsubscribing
// from broadcasting channel to receive live updates for lobby/game state.
package broadcaster

import "sync"

type Message struct {
	Type string
	// TODO: after I know whole design make this a proper type safety field
	Payload any
}

type subscriber struct {
	id int
	ch chan Message
}

type Broadcaster struct {
	mu          sync.RWMutex
	subscribers map[int]*subscriber
	nextId      int
}

func New(maxSubscribers int) *Broadcaster {
	return &Broadcaster{
		subscribers: make(map[int]*subscriber, maxSubscribers),
	}
}

func (b *Broadcaster) Subscribe() <-chan Message {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan Message, 10)
	b.subscribers[b.nextId] = &subscriber{ch: ch, id: b.nextId}
	b.nextId++
	return ch
}

func (b *Broadcaster) Unsubscribe(ch <-chan Message) {
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

func (b *Broadcaster) Broadcast(msg Message) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, sub := range b.subscribers {
		select {
		case sub.ch <- msg:
		default:
			// TODO: handle retransmitting?
		}
	}
}

func (b *Broadcaster) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for id, sub := range b.subscribers {
		close(sub.ch)
		delete(b.subscribers, id)
	}
}
