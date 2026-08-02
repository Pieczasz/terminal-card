// Package systemtest drives the server's real components end to end - catalog,
// registry, lobby manager, and game engine - through their public APIs only.
// It is deliberately black-box: no internals are reached into, so it exercises the
// same seams the SSH session layer uses.
package systemtest

import (
	"context"
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/lobby"
	"github.com/Pieczasz/terminal-card/internal/player"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	pokerGame = "Poker"

	// maxActions bounds the action driver so a rules change that stops a hand from
	// terminating fails the test instead of hanging it.
	maxActions = 60
)

// TestSystemRankedGameWithMidGameLeave walks the whole player journey: create a
// lobby, change its settings, fill it over a join code, start a real poker hand,
// have someone disconnect mid-hand, and finish with the ranked result recorded.
func TestSystemRankedGameWithMidGameLeave(t *testing.T) {
	t.Parallel()

	repo := newRankedFinalizeRecorder()
	manager := lobby.NewManager(context.Background(), repo)
	registry := realRegistry(t)

	leader := newPlayer(1, "alice")
	guests := []*player.Player{newPlayer(2, "bob"), newPlayer(3, "carol"), newPlayer(4, "dave")}

	// --- create -------------------------------------------------------------
	l, err := manager.New(leader,
		lobby.WithCardGame(&db.Game{Name: pokerGame}),
		lobby.WithMaxPlayers(2),
		lobby.WithPrivate(true),
	)
	require.NoError(t, err)
	require.Equal(t, leader, l.Leader())

	// --- change settings ----------------------------------------------------
	require.NoError(t, l.SetMaxPlayers(leader, 4, 2, 9))
	require.NoError(t, l.SetPrivate(leader, false))
	require.NoError(t, l.SetRanked(leader, true))
	assert.Equal(t, 4, l.MaxPlayers())
	assert.False(t, l.IsPrivate())
	assert.True(t, l.IsRanked())

	// A guest must not be able to change the lobby's settings.
	require.Error(t, l.SetMaxPlayers(guests[0], 9, 2, 9), "only the leader may change settings")

	// --- fill over the join code -------------------------------------------
	for _, g := range guests {
		require.NoError(t, manager.JoinLobbyByCode(l.Code(), g), "guest %s should join", g.ID)
	}
	require.Equal(t, 4, l.CurrentPlayers())
	assert.Contains(t, manager.PublicLobbies(leader), l, "a public waiting lobby is listed")

	events := l.Subscribe(leader.ID)
	t.Cleanup(func() { l.Unsubscribe(leader.ID, events) })

	// --- everyone readies up; the last one starts the game ------------------
	all := append([]*player.Player{leader}, guests...)
	for _, p := range all {
		require.NoError(t, l.ToggleReady(p, registry))
	}

	engine := awaitGameStart(t, events)
	require.NotNil(t, engine)
	assert.False(t, l.IsWaiting(), "a started lobby is no longer waiting")

	startingChips := chipsInPlay(t, engine)
	require.Positive(t, startingChips)

	// --- play, then drop a player mid-hand ----------------------------------
	require.True(t, actOnce(engine), "the first player to act should have a legal move")

	leaver := guests[0]
	manager.LeaveLobby(leaver)
	assert.False(t, l.HasPlayer(leaver), "leaving removes the player from the lobby")
	assert.Equal(t, startingChips, chipsInPlay(t, engine),
		"a mid-hand disconnect must not create or destroy chips")

	playOutMatch(t, engine)

	// --- the match resolved cleanly -----------------------------------------
	assert.Equal(t, startingChips, chipsInPlay(t, engine), "chips survive the whole match")
	standings := engine.StandingsIDs()
	assert.NotEmpty(t, standings, "a finished hand ranks its players")
	assert.Contains(t, standings, leaver.ID, "a player who left still places")

	// Wait for the recorder's signal instead of polling: the write is done when the
	// repository says so, with no timing assumption.
	repo.awaitFinalize(t)
	require.True(t, manager.WaitForFinalizers(5*time.Second), "ranked writes should drain")

	finalized := repo.calls()
	require.Len(t, finalized, 1, "a ranked hand finalizes exactly once")
	assert.Equal(t, pokerGame, finalized[0].gameName)
	assert.Len(t, finalized[0].userIDs, len(all),
		"every player is ranked, including the one who left mid-hand")
}

// A lobby must not start with fewer players than the game's own minimum, and must
// not accept more than its configured maximum.
func TestSystemLobbyRespectsGameBounds(t *testing.T) {
	t.Parallel()

	manager := lobby.NewManager(context.Background(), newRankedFinalizeRecorder())
	registry := realRegistry(t)

	leader := newPlayer(1, "alice")
	l, err := manager.New(leader,
		lobby.WithCardGame(&db.Game{Name: pokerGame}),
		lobby.WithMaxPlayers(2),
	)
	require.NoError(t, err)

	// Readying alone marks the player ready but refuses to start: poker needs 2+.
	err = l.ToggleReady(leader, registry)
	require.ErrorContains(t, err, "at least 2 players")
	assert.True(t, l.IsReady(leader), "the ready flag still toggles")
	assert.True(t, l.IsWaiting(), "one ready player cannot start a two-player game")

	require.NoError(t, manager.JoinLobbyByCode(l.Code(), newPlayer(2, "bob")))
	assert.Error(t, manager.JoinLobbyByCode(l.Code(), newPlayer(3, "carol")),
		"a full lobby rejects further joins")
}

// The leader leaving hands the lobby to a guest rather than stranding it.
func TestSystemLeaderLeavingPromotesGuest(t *testing.T) {
	t.Parallel()

	manager := lobby.NewManager(context.Background(), newRankedFinalizeRecorder())
	leader := newPlayer(1, "alice")
	guest := newPlayer(2, "bob")

	l, err := manager.New(leader, lobby.WithCardGame(&db.Game{Name: pokerGame}), lobby.WithMaxPlayers(4))
	require.NoError(t, err)
	require.NoError(t, manager.JoinLobbyByCode(l.Code(), guest))

	manager.LeaveLobby(leader)
	assert.Equal(t, guest, l.Leader(), "the remaining guest takes over")
	assert.True(t, l.IsLeader(guest))

	manager.LeaveLobby(guest)
	_, err = manager.FindLobbyByCode(l.Code())
	assert.Error(t, err, "an empty lobby is cleaned up")
}
