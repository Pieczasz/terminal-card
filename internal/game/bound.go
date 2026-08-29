package game

import (
	"errors"
	"fmt"
	"time"

	"github.com/Pieczasz/terminal-card/internal/deck"
)

// BoundEngine is a session-scoped handle: it only submits as its player and only
// returns that player's hand. Bind is the sole constructor and refuses a nil
// engine, so a non-nil BoundEngine always has one.
type BoundEngine struct {
	engine   *Engine
	playerID string
}

func Bind(engine *Engine, playerID string) *BoundEngine {
	if engine == nil {
		return nil
	}
	return &BoundEngine{engine: engine, playerID: playerID}
}

// Engine is the escape hatch to whole-table state, for views that genuinely need it:
// a card table renders every seat, and no per-player handle can express that. It is
// not a hole to route around Bind - reaching for it means you take on the redaction
// yourself, as poker's buildSeats does. Everything else belongs on BoundEngine.
func (b *BoundEngine) Engine() *Engine {
	if b == nil {
		return nil
	}
	return b.engine
}

func (b *BoundEngine) PlayerID() string {
	if b == nil {
		return ""
	}
	return b.playerID
}

// Subscribe joins the engine's event feed. The whole broadcaster is deliberately
// not reachable from here: a view only ever needs its own channel, and handing it
// the broadcaster would let it Broadcast or Close the feed for the whole table.
func (b *BoundEngine) Subscribe() (<-chan Event, error) {
	if b == nil {
		return nil, errors.New("no active game")
	}
	ch, err := b.engine.broadcaster.Subscribe()
	if err != nil {
		return nil, fmt.Errorf("subscribe to game events: %w", err)
	}
	return ch, nil
}

func (b *BoundEngine) Unsubscribe(events <-chan Event) {
	if b == nil || events == nil {
		return
	}
	b.engine.broadcaster.Unsubscribe(events)
}

func (b *BoundEngine) Submit(action Action) error {
	if b == nil {
		return errors.New("no active game")
	}
	return b.engine.SubmitAction(b.playerID, action)
}

// Frame is one consistent read of everything a view renders - the public snapshot,
// this player's hand, and the turn clock - plus, through fn (which may be nil), the
// per-game slice of engine state. One lock hold, so the pieces cannot describe
// different moments the way separate reads can.
//
// What fn is handed is State.Extra: unredacted table state - every other player's
// hand included, where the rules keep one there - so a view that reaches in takes
// on the redaction itself. It is also live and mutable: treat it as read-only and
// copy anything kept past the callback.
func (b *BoundEngine) Frame(fn func(extra any)) (StateSnapshot, []deck.Card, time.Duration) {
	if b == nil {
		return StateSnapshot{}, nil, 0
	}
	return b.engine.Frame(b.playerID, fn)
}
