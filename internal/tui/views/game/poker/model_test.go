package poker

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/poker"
	"github.com/Pieczasz/terminal-card/internal/player"
	"github.com/Pieczasz/terminal-card/internal/tui/router"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func testUser(id uint, name string) *db.User {
	return &db.User{Model: gorm.Model{ID: id}, Username: name}
}

// startedTable returns a two-handed table mid-hand plus the view bound to seat 1.
func startedTable(t *testing.T) (*game.Engine, *Model) {
	t.Helper()
	players := []*player.Player{
		{ID: "1", DatabaseUser: testUser(1, "alice")},
		{ID: "2", DatabaseUser: testUser(2, "bob")},
	}
	engine := game.NewEngine(&logic.Rules{}, players, deck.StandardDeck())
	require.NoError(t, engine.Start())

	global := router.GlobalContext{User: testUser(1, "alice")}
	m, ok := New(global, engine).(*Model)
	require.True(t, ok)
	return engine, m
}

func TestSyncState_BuildsSeatsFromEngine(t *testing.T) {
	t.Parallel()
	_, m := startedTable(t)

	require.Len(t, m.seats, 2)
	assert.Equal(t, logic.DefaultSmallBlind+logic.DefaultBigBlind, m.pot)
	assert.Equal(t, "PREFLOP", m.street)
	assert.False(t, m.handDone)
	assert.Equal(t, logic.DefaultBigBlind, m.minRaise)

	hero := m.heroSeat()
	require.NotNil(t, hero)
	assert.Equal(t, "1", hero.PlayerID)
	assert.Equal(t, "alice", hero.Name)
	assert.Len(t, hero.Hole, 2, "hero sees their own hole cards")

	for _, s := range m.seats {
		if s.IsHero {
			continue
		}
		assert.Empty(t, s.Hole, "opponent hole cards stay hidden before showdown")
		assert.Equal(t, 2, s.HandSize)
	}
}

// Seat flags drive the whole table render. Assert what a player can observe - who
// paid which blind, and that the markers are unique - rather than re-stating the
// assignment in syncState, which no change to the code could ever contradict.
func TestSyncState_SeatFlagsMatchBlinds(t *testing.T) {
	t.Parallel()
	_, m := startedTable(t)

	var sb, bb, dealer, turns int
	for _, s := range m.seats {
		if s.IsSB {
			sb++
			assert.Equal(t, logic.DefaultSmallBlind, s.Bet, "the small-blind seat posted the small blind")
		}
		if s.IsBB {
			bb++
			assert.Equal(t, logic.DefaultBigBlind, s.Bet, "the big-blind seat posted the big blind")
		}
		if s.IsDealer {
			dealer++
		}
		if s.IsTurn {
			turns++
		}
	}

	assert.Equal(t, 1, sb, "exactly one small blind")
	assert.Equal(t, 1, bb, "exactly one big blind")
	assert.Equal(t, 1, dealer, "exactly one dealer button")
	assert.Equal(t, 1, turns, "exactly one seat is on turn mid-hand")

	// Heads-up, the button posts the small blind.
	hero := m.heroSeat()
	require.NotNil(t, hero)
	for _, s := range m.seats {
		if s.IsDealer {
			assert.True(t, s.IsSB, "heads-up: the dealer is the small blind")
		}
	}
}

func TestSyncState_ChipsAndBetsSumToStacks(t *testing.T) {
	t.Parallel()
	_, m := startedTable(t)

	var total uint
	for _, s := range m.seats {
		total += s.Chips + s.Bet
	}
	assert.Equal(t, 2*logic.DefaultStack, total, "blinds move chips into bets, never destroy them")
}

func TestClampRaise_BoundsToLegalRange(t *testing.T) {
	t.Parallel()
	_, m := startedTable(t)

	// Hardcoded rather than recomputed from m: deriving the expectation from
	// currentBet+minRaise and streetBetMax would restate clampRaise's own body, and
	// swapping its min and max would still pass. Heads-up with DefaultStack=1000,
	// SB=25 and BB=50 the legal band is exactly [100, 1000].
	const wantMin, wantMax = uint(100), uint(1000)

	assert.Equal(t, wantMin, m.clampRaise(0), "below the minimum raises up to it")
	assert.Equal(t, wantMax, m.clampRaise(50_000), "above the stack clamps down to it")
	assert.Equal(t, uint(500), m.clampRaise(500), "an in-range amount passes through")
}

func TestSyncState_NilBoundIsInert(t *testing.T) {
	t.Parallel()
	m, ok := New(router.GlobalContext{}, nil).(*Model)
	require.True(t, ok)

	assert.Empty(t, m.seats)
	assert.Zero(t, m.pot)
	assert.False(t, m.canFold())
	assert.False(t, m.canRaise())
	assert.False(t, m.canAllIn())
}

// syncState rebuilds every seat from the engine on each broadcast event, so it runs
// once per player per action. It shares the frame budget (~16ms) with the lipgloss
// render, so it needs to stay in the microseconds.
func BenchmarkSyncState(b *testing.B) {
	for _, seats := range []int{2, 6, 9} {
		b.Run(fmt.Sprintf("seats=%d", seats), func(b *testing.B) {
			players := make([]*player.Player, 0, seats)
			for i := range seats {
				players = append(players, &player.Player{
					ID:           strconv.Itoa(i + 1),
					DatabaseUser: testUser(uint(i+1), fmt.Sprintf("p%d", i+1)),
				})
			}
			engine := game.NewEngine(&logic.Rules{}, players, deck.StandardDeck())
			if err := engine.Start(); err != nil {
				b.Fatal(err)
			}
			b.Cleanup(engine.Close)

			m, ok := New(router.GlobalContext{User: testUser(1, "p1")}, engine).(*Model)
			if !ok {
				b.Fatal("New did not return *Model")
			}

			b.ReportAllocs()
			for b.Loop() {
				m.syncState()
			}
		})
	}
}
