package game

import (
	"errors"
	"slices"
	"time"

	"github.com/Pieczasz/terminal-card/internal/broadcaster"
	"github.com/Pieczasz/terminal-card/internal/deck"
)

// BoundEngine exposes game operations bound to an authenticated player ID.
// TUI views should use this instead of calling SubmitAction/WithState on the
// shared *Engine so foreign player IDs and other hands are not reachable.
type BoundEngine struct {
	engine   *Engine
	playerID string
}

// Bind returns a session-scoped handle for playerID.
func Bind(engine *Engine, playerID string) *BoundEngine {
	if engine == nil {
		return nil
	}
	return &BoundEngine{engine: engine, playerID: playerID}
}

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

func (b *BoundEngine) Broadcaster() *broadcaster.Broadcaster[Event] {
	if b == nil || b.engine == nil {
		return nil
	}
	return b.engine.Broadcaster()
}

// Submit applies an action as the bound player only.
func (b *BoundEngine) Submit(action Action) error {
	if b == nil || b.engine == nil {
		return errors.New("no active game")
	}
	return b.engine.SubmitAction(b.playerID, action)
}

// Snapshot returns a redacted view for the bound player.
func (b *BoundEngine) Snapshot() StateSnapshot {
	if b == nil || b.engine == nil {
		return StateSnapshot{}
	}
	return b.engine.Snapshot()
}

// Hand returns a defensive copy of the bound player's cards.
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

// WithExtra runs fn with the game Extra value under the state lock.
func (b *BoundEngine) WithExtra(fn func(extra any)) {
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

// IsFinished reports whether the bound engine's game is finished.
func (b *BoundEngine) IsFinished() bool {
	if b == nil || b.engine == nil {
		return true
	}
	return b.engine.IsFinished()
}
