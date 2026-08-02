//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserRepository_RegisterUserWithKey(t *testing.T) {
	t.Parallel()
	database := testutil.SetupTestDB(t, &db.User{}, &db.PublicKey{}, &db.Ranking{})
	repo := NewUserRepository(database)

	// Each subtest seeds whatever it needs and uses its own identifiers. Previously
	// "username already taken" relied on the subtest above it having run, so it
	// failed when executed on its own.
	t.Run("successful registration", func(t *testing.T) {
		t.Parallel()
		user, key, err := repo.RegisterUserWithKey(context.Background(), "reg_ok", "fp_reg_ok")
		require.NoError(t, err)
		require.NotNil(t, user)
		assert.Equal(t, "reg_ok", user.Username)
		assert.Equal(t, "fp_reg_ok", key.Fingerprint)
	})

	t.Run("username already taken", func(t *testing.T) {
		t.Parallel()
		_, _, err := repo.RegisterUserWithKey(context.Background(), "dup_name", "fp_name_1")
		require.NoError(t, err, "seed the name first")

		_, _, err = repo.RegisterUserWithKey(context.Background(), "dup_name", "fp_name_2")
		require.ErrorContains(t, err, "username already taken")
	})

	t.Run("invalid username length", func(t *testing.T) {
		t.Parallel()
		_, _, err := repo.RegisterUserWithKey(context.Background(), "this_username_is_way_too_long", "fp_too_long")
		require.ErrorContains(t, err, "username cannot exceed 16 characters")
	})

	t.Run("duplicate fingerprint", func(t *testing.T) {
		t.Parallel()
		_, _, err := repo.RegisterUserWithKey(context.Background(), "fp_owner", "fp_shared")
		require.NoError(t, err, "seed the fingerprint first")

		_, _, err = repo.RegisterUserWithKey(context.Background(), "fp_thief", "fp_shared")
		require.ErrorContains(t, err, "public key already registered")
	})
}

func TestUserRepository_RegisterUserWithKey_ConcurrentSameFingerprint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	database := testutil.SetupTestDB(t, &db.User{}, &db.PublicKey{}, &db.Ranking{})
	repo := NewUserRepository(database)

	const workers = 8
	errs := make(chan error, workers)
	for i := range workers {
		go func(i int) {
			_, _, err := repo.RegisterUserWithKey(context.Background(), fmt.Sprintf("user_%d", i), "shared_fingerprint")
			errs <- err
		}(i)
	}

	var success, fail int
	for range workers {
		err := <-errs
		if err == nil {
			success++
		} else {
			fail++
		}
	}
	assert.Equal(t, 1, success)
	assert.Equal(t, workers-1, fail)

	var userCount int64
	database.Model(&db.User{}).Count(&userCount)
	assert.Equal(t, int64(1), userCount)
}

func TestUserRepository_LoadUserByFingerprint(t *testing.T) {
	t.Parallel()
	database := testutil.SetupTestDB(t, &db.User{}, &db.PublicKey{}, &db.Ranking{})
	repo := NewUserRepository(database)

	_, _, err := repo.RegisterUserWithKey(context.Background(), "player_two", "fingerprint_abc")
	assert.NoError(t, err)

	t.Run("existing user", func(t *testing.T) {
		user, key, err := repo.LoadUserByFingerprint(context.Background(), "fingerprint_abc")
		assert.NoError(t, err)
		assert.NotNil(t, user)
		assert.NotNil(t, key)
		assert.Equal(t, "player_two", user.Username)
	})

	t.Run("non-existent user", func(t *testing.T) {
		user, key, err := repo.LoadUserByFingerprint(context.Background(), "fingerprint_unknown")
		assert.NoError(t, err)
		assert.Nil(t, user)
		assert.Nil(t, key)
	})
}

func TestUserRepository_UpdateUserActivity(t *testing.T) {
	t.Parallel()
	database := testutil.SetupTestDB(t, &db.User{}, &db.PublicKey{}, &db.Ranking{})
	repo := NewUserRepository(database)

	user, key, err := repo.RegisterUserWithKey(context.Background(), "player_three", "fingerprint_xyz")
	require.NoError(t, err)

	// Backdate both timestamps. Asserting only "After or Equal" would pass even if
	// UpdateUserActivity did nothing at all, since Equal covers the no-op.
	stale := time.Now().Add(-24 * time.Hour).UTC()
	require.NoError(t, database.Model(&db.User{}).Where("id = ?", user.ID).
		Update("last_seen_at", stale).Error)
	require.NoError(t, database.Model(&db.PublicKey{}).Where("id = ?", key.ID).
		Update("last_used_at", stale).Error)

	repo.UpdateUserActivity(context.Background(), user, key)

	updatedUser, updatedKey, err := repo.LoadUserByFingerprint(context.Background(), "fingerprint_xyz")
	require.NoError(t, err)

	assert.True(t, updatedUser.LastSeenAt.After(stale),
		"last_seen_at must move forward: got %v, seeded %v", updatedUser.LastSeenAt, stale)
	assert.True(t, updatedKey.LastUsedAt.After(stale),
		"last_used_at must move forward: got %v, seeded %v", updatedKey.LastUsedAt, stale)
}

func TestUserRepository_BestPlayers(t *testing.T) {
	t.Parallel()
	database := testutil.SetupTestDB(t, &db.User{}, &db.PublicKey{}, &db.Ranking{}, &db.Game{})
	repo := NewUserRepository(database)
	ctx := context.Background()

	game := &db.Game{Name: "TestGame"}
	database.Create(game)

	for i := 1; i <= 5; i++ {
		u := &db.User{Username: "player" + string(rune(i+48))}
		database.Create(u)
		database.Create(&db.Ranking{UserID: u.ID, GameID: game.ID, Elo: uint32(1000 + i*100)})
	}

	best, err := repo.BestPlayers(ctx, 3)
	assert.NoError(t, err)
	assert.Len(t, best, 3)
	assert.Equal(t, uint32(1500), best[0].Elo)
	assert.Equal(t, uint32(1400), best[1].Elo)
	assert.Equal(t, uint32(1300), best[2].Elo)

	// Test cache
	bestCached, err := repo.BestPlayers(ctx, 2)
	assert.NoError(t, err)
	assert.Len(t, bestCached, 2)
}

func TestUserRepository_UserProfile(t *testing.T) {
	t.Parallel()
	database := testutil.SetupTestDB(t, &db.User{}, &db.PublicKey{}, &db.Ranking{}, &db.Game{})
	repo := NewUserRepository(database)
	ctx := context.Background()

	u, _, _ := repo.RegisterUserWithKey(ctx, "profile_user", "profile_fp")

	game := &db.Game{Name: "ProfileGame"}
	database.Create(game)
	database.Create(&db.Ranking{UserID: u.ID, GameID: game.ID, Elo: 1600})

	profile, err := repo.UserProfile(ctx, u.ID)
	assert.NoError(t, err)
	assert.Equal(t, "profile_user", profile.Username)
	assert.Len(t, profile.PublicKeys, 1)
	assert.Len(t, profile.Rankings, 1)
	assert.Equal(t, "ProfileGame", profile.Rankings[0].Game.Name)

	_, err = repo.UserProfile(ctx, 9999)
	assert.Error(t, err)
}

func TestUserRepository_UserMatchHistory(t *testing.T) {
	t.Parallel()
	database := testutil.SetupTestDB(t, &db.User{}, &db.PublicKey{}, &db.Ranking{}, &db.Game{}, &db.Match{}, &db.MatchParticipant{})
	repo := NewUserRepository(database)
	ctx := context.Background()

	u, _, _ := repo.RegisterUserWithKey(ctx, "history_user", "history_fp")

	game := &db.Game{Name: "HistoryGame"}
	database.Create(game)

	match := &db.Match{GameID: game.ID}
	database.Create(match)

	database.Create(&db.MatchParticipant{MatchID: match.ID, UserID: u.ID, Placement: 1, EloDelta: 15})

	history, err := repo.UserMatchHistory(ctx, u.ID, 10)
	assert.NoError(t, err)
	assert.Len(t, history, 1)
	assert.Equal(t, 1, history[0].Placement)
	assert.Equal(t, 15, history[0].EloDelta)
	assert.Equal(t, "HistoryGame", history[0].Match.Game.Name)
}
