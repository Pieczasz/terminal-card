//go:build integration

package systemtest

import (
	"context"
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/elo"
	"github.com/Pieczasz/terminal-card/internal/lobby"
	"github.com/Pieczasz/terminal-card/internal/player"
	"github.com/Pieczasz/terminal-card/internal/repository"
	"github.com/Pieczasz/terminal-card/internal/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// signallingMatchRepo wraps the real repository and announces each completed ranked
// write, so the test waits on the write itself instead of polling the database.
// The embedded interface supplies every other method unchanged.
type signallingMatchRepo struct {
	db.MatchRepository
	signal chan struct{}
}

func newSignallingMatchRepo(inner db.MatchRepository) *signallingMatchRepo {
	return &signallingMatchRepo{MatchRepository: inner, signal: make(chan struct{}, 8)}
}

func (s *signallingMatchRepo) FinalizeRankedMatch(ctx context.Context, gameName string, orderedUserIDs []uint) error {
	err := s.MatchRepository.FinalizeRankedMatch(ctx, gameName, orderedUserIDs)
	select {
	case s.signal <- struct{}{}:
	default:
	}
	return err
}

func (s *signallingMatchRepo) awaitFinalize(t *testing.T) {
	t.Helper()
	select {
	case <-s.signal:
	case <-time.After(30 * time.Second):
		t.Fatal("ranked finalize never ran")
	}
}

// TestSystem_RankedResultReachesLeaderboardAndProfile closes the loop the unit
// tiers cannot: a real ranked hand is played through the lobby and engine against a
// live Postgres, and the result has to show up in the two screens players read -
// the leaderboard and their own profile.
func TestSystem_RankedResultReachesLeaderboardAndProfile(t *testing.T) {
	gormDB := testutil.SetupTestDB(t,
		&db.User{}, &db.PublicKey{}, &db.Ranking{}, &db.Game{}, &db.Match{}, &db.MatchParticipant{})

	ctx := context.Background()
	userRepo := repository.NewUserRepository(gormDB)
	matchRepo := newSignallingMatchRepo(repository.NewMatchRepository(gormDB))
	manager := lobby.NewManager(ctx, matchRepo)
	registry := realRegistry(t)

	// Register real users the way the SSH layer does, by public key.
	players := make([]*player.Player, 0, 3)
	for _, name := range []string{"alice", "bob", "carol"} {
		user, _, err := userRepo.RegisterUserWithKey(ctx, name, "fingerprint-"+name)
		require.NoError(t, err)
		players = append(players, playerFor(user))
	}
	leader := players[0]

	l, err := manager.New(leader,
		lobby.WithCardGame(&db.Game{Name: pokerGame}),
		lobby.WithMaxPlayers(3),
		lobby.WithRanked(true),
	)
	require.NoError(t, err)
	for _, g := range players[1:] {
		require.NoError(t, manager.JoinLobbyByCode(l.Code(), g))
	}

	events := l.Subscribe(leader.ID)
	t.Cleanup(func() { l.Unsubscribe(leader.ID, events) })

	for _, p := range players {
		require.NoError(t, l.ToggleReady(p, registry))
	}
	engine := awaitGameStart(t, events)

	// Everyone shoves so the hand reaches showdown without needing a full strategy.
	for range maxActions {
		if engine.IsFinished() || handComplete(t, engine) {
			break
		}
		if !actOnce(engine) {
			break
		}
	}
	require.True(t, handComplete(t, engine) || engine.IsFinished(), "the hand must finish")

	// Wait for the write itself rather than polling the database.
	matchRepo.awaitFinalize(t)
	require.True(t, manager.WaitForFinalizers(30*time.Second), "ranked write must drain")

	best, err := userRepo.BestPlayers(ctx, 10)
	require.NoError(t, err)
	require.NotEmpty(t, best, "a finished ranked game must populate the leaderboard")

	var moved bool
	for _, r := range best {
		if r.Elo != uint32(elo.DefaultRating) {
			moved = true
		}
	}
	assert.True(t, moved, "at least one player's Elo must change after a ranked game")

	for _, p := range players {
		profile, err := userRepo.UserProfile(ctx, p.DatabaseUser.ID)
		require.NoError(t, err)
		assert.NotEmpty(t, profile.Rankings, "%s should have a ranking row", profile.Username)

		history, err := userRepo.UserMatchHistory(ctx, p.DatabaseUser.ID, 10)
		require.NoError(t, err)
		require.Len(t, history, 1, "%s should have exactly one recorded match", profile.Username)
		assert.Positive(t, history[0].Placement, "placement is 1-based")
		assert.Equal(t, pokerGame, history[0].Match.Game.Name)
	}
}

// A casual lobby must write nothing: no match history, no Elo movement.
func TestSystem_CasualGameWritesNothing(t *testing.T) {
	gormDB := testutil.SetupTestDB(t,
		&db.User{}, &db.PublicKey{}, &db.Ranking{}, &db.Game{}, &db.Match{}, &db.MatchParticipant{})

	ctx := context.Background()
	userRepo := repository.NewUserRepository(gormDB)
	manager := lobby.NewManager(ctx, repository.NewMatchRepository(gormDB))
	registry := realRegistry(t)

	players := make([]*player.Player, 0, 2)
	for _, name := range []string{"dave", "erin"} {
		user, _, err := userRepo.RegisterUserWithKey(ctx, name, "fingerprint-"+name)
		require.NoError(t, err)
		players = append(players, playerFor(user))
	}

	l, err := manager.New(players[0],
		lobby.WithCardGame(&db.Game{Name: pokerGame}),
		lobby.WithMaxPlayers(2),
		lobby.WithRanked(false),
	)
	require.NoError(t, err)
	require.NoError(t, manager.JoinLobbyByCode(l.Code(), players[1]))

	events := l.Subscribe(players[0].ID)
	t.Cleanup(func() { l.Unsubscribe(players[0].ID, events) })
	for _, p := range players {
		require.NoError(t, l.ToggleReady(p, registry))
	}
	engine := awaitGameStart(t, events)

	for range maxActions {
		if engine.IsFinished() || handComplete(t, engine) {
			break
		}
		if !actOnce(engine) {
			break
		}
	}
	require.True(t, manager.WaitForFinalizers(10*time.Second))

	for _, p := range players {
		history, err := userRepo.UserMatchHistory(ctx, p.DatabaseUser.ID, 10)
		require.NoError(t, err)
		assert.Empty(t, history, "a casual game must not be recorded")
	}
}
