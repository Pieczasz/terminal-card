package poker

import (
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

// Seat flags drive the whole table render, so pin the ones a split could scramble.
func TestSyncState_SeatFlagsMatchBlinds(t *testing.T) {
	t.Parallel()
	engine, m := startedTable(t)

	var sb, bb, dealer, turns int
	engine.WithState(func(state *game.State) {
		extra, ok := state.Extra.(*logic.State)
		require.True(t, ok)
		for i, s := range m.seats {
			assert.Equal(t, i == extra.SBIndex, s.IsSB)
			assert.Equal(t, i == extra.BBIndex, s.IsBB)
			assert.Equal(t, i == extra.DealerIndex, s.IsDealer)
		}
	})
	for _, s := range m.seats {
		if s.IsSB {
			sb++
		}
		if s.IsBB {
			bb++
		}
		if s.IsDealer {
			dealer++
		}
		if s.IsTurn {
			turns++
		}
	}
	assert.Equal(t, 1, sb)
	assert.Equal(t, 1, bb)
	assert.Equal(t, 1, dealer)
	assert.Equal(t, 1, turns, "exactly one seat is on turn mid-hand")
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

	minTo := m.currentBet + m.minRaise
	maxTo := m.streetBetMax()
	require.Positive(t, maxTo)

	assert.Equal(t, minTo, m.clampRaise(0), "below the minimum raises up to it")
	assert.Equal(t, maxTo, m.clampRaise(maxTo+1_000), "above the stack clamps down to it")
	if minTo < maxTo {
		assert.Equal(t, minTo+1, m.clampRaise(minTo+1), "in-range amounts pass through")
	}
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
