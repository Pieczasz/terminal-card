package game

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/Pieczasz/terminal-card/internal/deck"
)

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
	if b == nil || b.engine == nil {
		return nil, errors.New("no active game")
	}
	ch, err := b.engine.broadcaster.Subscribe()
	if err != nil {
		return nil, fmt.Errorf("subscribe to game events: %w", err)
	}
	return ch, nil
}

func (b *BoundEngine) Unsubscribe(events <-chan Event) {
	if b == nil || b.engine == nil || events == nil {
		return
	}
	b.engine.broadcaster.Unsubscribe(events)
}

func (b *BoundEngine) Submit(action Action) error {
	if b == nil || b.engine == nil {
		return errors.New("no active game")
	}
	return b.engine.SubmitAction(b.playerID, action)
}

func (b *BoundEngine) IsMyTurn() bool {
	if b == nil || b.engine == nil {
		return false
	}
	return b.engine.CurrentPlayerID() == b.playerID
}

func (b *BoundEngine) Snapshot() StateSnapshot {
	if b == nil || b.engine == nil {
		return StateSnapshot{}
	}
	return b.engine.Snapshot()
}

func (b *BoundEngine) Hand() []deck.Card {
	if b == nil || b.engine == nil {
		return nil
	}
	var hand []deck.Card
	b.engine.WithState(func(state *State) {
		for _, p := range state.Players {
			if p != nil && p.ID == b.playerID {
				hand = slices.Clone(p.Cards)
				return
			}
		}
	})
	return hand
}

// WithHiddenState reads the per-game slice of engine state under the engine lock.
// State.Extra is unredacted table state - every other player's hand included, where
// the rules keep one there - so a view that reaches in takes on the redaction itself.
func (b *BoundEngine) WithHiddenState(fn func(extra any)) {
	if b == nil || b.engine == nil || fn == nil {
		return
	}
	b.engine.WithState(func(state *State) {
		fn(state.Extra)
	})
}

func (b *BoundEngine) TurnRemaining() time.Duration {
	if b == nil || b.engine == nil {
		return 0
	}
	deadline := b.engine.TurnDeadline()
	if deadline.IsZero() {
		return 0
	}
	return max(time.Until(deadline), 0)
}
