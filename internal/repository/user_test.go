//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/testutil"

	"github.com/stretchr/testify/assert"
)

func TestUserRepository_RegisterUserWithKey(t *testing.T) {
	t.Parallel()
	database := testutil.SetupTestDB(t, &db.User{}, &db.PublicKey{}, &db.Ranking{})
	repo := NewUserRepository(database)

	t.Run("successful registration", func(t *testing.T) {
		user, key, err := repo.RegisterUserWithKey(context.Background(), "player_one", "fingerprint_1")
		assert.NoError(t, err)
		assert.NotNil(t, user)
		assert.Equal(t, "player_one", user.Username)
		assert.Equal(t, "fingerprint_1", key.Fingerprint)
	})

	t.Run("username already taken", func(t *testing.T) {
		_, _, err := repo.RegisterUserWithKey(context.Background(), "player_one", "fingerprint_2")
		assert.ErrorContains(t, err, "username already taken")
	})

	t.Run("invalid username length", func(t *testing.T) {
		_, _, err := repo.RegisterUserWithKey(context.Background(), "this_username_is_way_too_long", "fingerprint_3")
		assert.ErrorContains(t, err, "username cannot exceed 16 characters")
	})

	t.Run("duplicate fingerprint", func(t *testing.T) {
		_, _, err := repo.RegisterUserWithKey(context.Background(), "player_dup", "fingerprint_1")
		assert.ErrorContains(t, err, "public key already registered")
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
	assert.NoError(t, err)

	initialSeenAt := user.LastSeenAt
	initialUsedAt := key.LastUsedAt

	repo.UpdateUserActivity(context.Background(), user, key)

	updatedUser, updatedKey, err := repo.LoadUserByFingerprint(context.Background(), "fingerprint_xyz")
	assert.NoError(t, err)

	assert.True(t, updatedUser.LastSeenAt.After(initialSeenAt) || updatedUser.LastSeenAt.Equal(initialSeenAt))
	assert.True(t, updatedKey.LastUsedAt.After(initialUsedAt) || updatedKey.LastUsedAt.Equal(initialUsedAt))
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
