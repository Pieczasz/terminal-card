package systemtest

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/catalog"
	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/game/poker"
	"github.com/Pieczasz/terminal-card/internal/lobby"
	"github.com/Pieczasz/terminal-card/internal/player"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type finalizedMatch struct {
	gameName string
	userIDs  []uint
}

// rankedFinalizeRecorder narrows the persistence boundary to the ranked write
// exercised by the non-database system tests. The recorder signals each call so
// tests wait for the write rather than scheduling or polling.
type rankedFinalizeRecorder struct {
	db.MatchRepository

	mu        sync.Mutex
	finalized []finalizedMatch
	signal    chan struct{}
}

func newRankedFinalizeRecorder() *rankedFinalizeRecorder {
	return &rankedFinalizeRecorder{signal: make(chan struct{}, 8)}
}

func (r *rankedFinalizeRecorder) FinalizeRankedMatch(
	ctx context.Context,
	gameName string,
	orderedUserIDs []uint,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	r.finalized = append(r.finalized, finalizedMatch{
		gameName: gameName,
		userIDs:  append([]uint(nil), orderedUserIDs...),
	})
	r.mu.Unlock()

	select {
	case r.signal <- struct{}{}:
	default: // no test is waiting; never block the game
	}
	return nil
}

func (r *rankedFinalizeRecorder) awaitFinalize(t *testing.T) {
	t.Helper()
	select {
	case <-r.signal:
	case <-time.After(10 * time.Second):
		t.Fatal("ranked finalize never ran")
	}
}

func (r *rankedFinalizeRecorder) calls() []finalizedMatch {
	r.mu.Lock()
	defer r.mu.Unlock()

	calls := make([]finalizedMatch, len(r.finalized))
	for i, call := range r.finalized {
		calls[i] = finalizedMatch{
			gameName: call.gameName,
			userIDs:  append([]uint(nil), call.userIDs...),
		}
	}
	return calls
}

func realRegistry(t *testing.T) *game.Registry {
	t.Helper()
	registry := game.NewRegistry()
	for _, entry := range catalog.All {
		registry.RegisterModule(entry.Module())
	}
	return registry
}

func newPlayer(id uint, name string) *player.Player {
	return playerFor(&db.User{Model: gorm.Model{ID: id}, Username: name})
}

func playerFor(user *db.User) *player.Player {
	return &player.Player{ID: fmt.Sprint(user.ID), DatabaseUser: user}
}

func awaitGameStart(t *testing.T, events <-chan lobby.Event) *game.Engine {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("lobby channel closed before the game started")
			}
			if event.Type != lobby.EventGameStarted {
				continue
			}
			engine, ok := event.Payload.(*game.Engine)
			require.True(t, ok, "GAME_STARTED payload must carry the engine")
			return engine
		case <-deadline:
			t.Fatal("timed out waiting for the game to start")
		}
	}
}

func chipsInPlay(t *testing.T, engine *game.Engine) uint {
	t.Helper()
	var total uint
	engine.WithState(func(state *game.State) {
		extra, ok := state.Extra.(*poker.State)
		require.True(t, ok)
		total = extra.MainPool
		for _, chips := range extra.PlayerChips {
			total += chips
		}
	})
	return total
}

func playOutMatch(t *testing.T, engine *game.Engine) {
	t.Helper()
	for range maxActions * poker.HandsPerMatch {
		if engine.IsFinished() {
			return
		}
		if !actOnce(engine) {
			break
		}
	}
	require.True(t, engine.IsFinished(), "the match must reach its end")
}

func actOnce(engine *game.Engine) bool {
	playerID := engine.CurrentPlayerID()
	for _, action := range []game.Action{
		poker.ActionCheck{},
		poker.ActionCall{},
		poker.ActionFold{},
		poker.ActionNextHand{},
	} {
		if err := engine.SubmitAction(playerID, action); err == nil {
			return true
		}
	}
	return false
}
