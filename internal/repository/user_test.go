//go:build integration

package repository_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/repository"
	"github.com/Pieczasz/terminal-card/internal/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserRepository_RegisterUserWithKey(t *testing.T) {
	t.Parallel()
	database := testutil.SetupTestDB(t)
	repo := repository.NewUserRepository(database)

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
	database := testutil.SetupTestDB(t)
	repo := repository.NewUserRepository(database)

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
	database := testutil.SetupTestDB(t)
	repo := repository.NewUserRepository(database)

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
	database := testutil.SetupTestDB(t)
	repo := repository.NewUserRepository(database)

	user, key, err := repo.RegisterUserWithKey(context.Background(), "player_three", "fingerprint_xyz")
	require.NoError(t, err)

	// Backdate both timestamps. Asserting only "After or Equal" would pass even if
	// UpdateUserActivity did nothing at all, since Equal covers the no-op.
	stale := time.Now().Add(-24 * time.Hour).UTC()
	require.NoError(t, database.Model(&db.User{}).Where("id = ?", user.ID).
		Update("last_seen_at", stale).Error)
	require.NoError(t, database.Model(&db.PublicKey{}).Where("id = ?", key.ID).
		Update("last_used_at", stale).Error)

	require.NoError(t, repo.UpdateUserActivity(context.Background(), user, key))

	updatedUser, updatedKey, err := repo.LoadUserByFingerprint(context.Background(), "fingerprint_xyz")
	require.NoError(t, err)

	assert.True(t, updatedUser.LastSeenAt.After(stale),
		"last_seen_at must move forward: got %v, seeded %v", updatedUser.LastSeenAt, stale)
	assert.True(t, updatedKey.LastUsedAt.After(stale),
		"last_used_at must move forward: got %v, seeded %v", updatedKey.LastUsedAt, stale)
}

// Reproduction: soft-deleting a user leaves its public_keys row matching, and the
// Preload's deleted_at IS NULL only empties the association. The old code returned
// &dbKey.User unconditionally, so the ssh auth path saw a non-nil user with ID 0 and
// logged the deleted account in as user zero.
func TestUserRepository_SoftDeletedUserDoesNotAuthenticate(t *testing.T) {
	t.Parallel()
	database := testutil.SetupTestDB(t)
	repo := repository.NewUserRepository(database)
	ctx := context.Background()

	created, createdKey, err := repo.RegisterUserWithKey(ctx, "activity_user", "activity_fp")
	require.NoError(t, err)
	require.NoError(t, database.Delete(&db.User{}, created.ID).Error)

	// The key row is deliberately left behind: that is the state the fix is about.
	var key db.PublicKey
	require.NoError(t, database.First(&key, createdKey.ID).Error)

	user, loadedKey, err := repo.LoadUserByFingerprint(ctx, "activity_fp")
	require.NoError(t, err, "a deleted account is a miss, not an error")
	assert.Nil(t, user, "a soft-deleted user must not authenticate")
	assert.Nil(t, loadedKey, "and its key must not come back either")

	var users []db.User
	require.NoError(t, database.Unscoped().Find(&users).Error)
	require.Len(t, users, 1)
	assert.True(t, users[0].DeletedAt.Valid, "a deleted user must not come back on login")
}

// The public key carries a preloaded User, which GORM would happily upsert
// alongside the timestamp - so every login became a write to the users table.
func TestUserRepository_UpdateUserActivityDoesNotWriteUsers(t *testing.T) {
	t.Parallel()
	database := testutil.SetupTestDB(t)
	repo := repository.NewUserRepository(database)
	ctx := context.Background()

	_, createdKey, err := repo.RegisterUserWithKey(ctx, "activity_two", "activity_fp2")
	require.NoError(t, err)

	stale := time.Now().Add(-24 * time.Hour).UTC()
	require.NoError(t, database.Model(&db.PublicKey{}).Where("id = ?", createdKey.ID).
		Update("last_used_at", stale).Error)

	user, key, err := repo.LoadUserByFingerprint(ctx, "activity_fp2")
	require.NoError(t, err)
	require.NotNil(t, user)
	require.NoError(t, repo.UpdateUserActivity(ctx, user, key))

	var reloaded db.PublicKey
	require.NoError(t, database.First(&reloaded, createdKey.ID).Error)
	assert.True(t, reloaded.LastUsedAt.After(stale),
		"the association write must not take the key's own update down with it")

	var users int64
	require.NoError(t, database.Model(&db.User{}).Count(&users).Error)
	assert.Equal(t, int64(1), users, "updating a key must not insert a user")
}

func TestUserRepository_UserMatchHistoryRejectsNegativeLimit(t *testing.T) {
	t.Parallel()
	database := testutil.SetupTestDB(t)
	repo := repository.NewUserRepository(database)
	ctx := context.Background()

	u, _, err := repo.RegisterUserWithKey(ctx, "neg_limit", "neg_limit_fp")
	require.NoError(t, err)

	game := &db.Game{Name: "NegGame"}
	require.NoError(t, database.Create(game).Error)
	match := &db.Match{GameID: game.ID}
	require.NoError(t, database.Create(match).Error)
	require.NoError(t, database.Create(&db.MatchParticipant{MatchID: match.ID, UserID: u.ID, Placement: 1}).Error)

	// GORM treats a negative Limit as "no limit", which would stream the whole table.
	history, err := repo.UserMatchHistory(ctx, u.ID, -1)
	require.NoError(t, err)
	assert.Empty(t, history)
}

func TestUserRepository_BestPlayersCachesShortTables(t *testing.T) {
	t.Parallel()
	database := testutil.SetupTestDB(t)
	repo := repository.NewUserRepository(database)
	ctx := context.Background()

	game := &db.Game{Name: "ShortTable"}
	require.NoError(t, database.Create(game).Error)
	u := &db.User{Username: "only_player"}
	require.NoError(t, database.Create(u).Error)
	require.NoError(t, database.Create(&db.Ranking{UserID: u.ID, GameID: game.ID, Elo: 1700}).Error)

	best, err := repo.BestPlayers(ctx, 25, "")
	require.NoError(t, err)
	require.Len(t, best, 1)

	// Fewer rankings than the limit still counts as a warm cache, so pulling the
	// rows out from under it must not change the answer.
	require.NoError(t, database.Where("1 = 1").Delete(&db.Ranking{}).Error)

	cached, err := repo.BestPlayers(ctx, 25, "")
	require.NoError(t, err)
	assert.Len(t, cached, 1, "a table shorter than the limit must still be cached")
}

// A limit above bestPlayersCacheSize (200) cannot be answered from an entry that
// only holds 200 rows, so it must bypass the cache in both directions rather than
// serve - or store - a silently truncated board.
func TestUserRepository_BestPlayersBypassesCacheAboveItsSize(t *testing.T) {
	t.Parallel()
	database := testutil.SetupTestDB(t)
	repo := repository.NewUserRepository(database)
	ctx := context.Background()

	game := &db.Game{Name: "BigAsk"}
	require.NoError(t, database.Create(game).Error)
	u := &db.User{Username: "big_ask"}
	require.NoError(t, database.Create(u).Error)
	require.NoError(t, database.Create(&db.Ranking{UserID: u.ID, GameID: game.ID, Elo: 1700}).Error)

	best, err := repo.BestPlayers(ctx, 201, "")
	require.NoError(t, err)
	require.Len(t, best, 1)

	require.NoError(t, database.Where("1 = 1").Delete(&db.Ranking{}).Error)

	// Had the oversized ask been cached, this would still answer 1.
	again, err := repo.BestPlayers(ctx, 201, "")
	require.NoError(t, err)
	assert.Empty(t, again, "an oversized limit must not be served from the cache")
}

func TestUserRepository_BestPlayers(t *testing.T) {
	t.Parallel()
	database := testutil.SetupTestDB(t)
	repo := repository.NewUserRepository(database)
	ctx := context.Background()

	game := &db.Game{Name: "TestGame"}
	database.Create(game)

	for i := 1; i <= 5; i++ {
		u := &db.User{Username: "player" + string(rune(i+48))}
		database.Create(u)
		database.Create(&db.Ranking{UserID: u.ID, GameID: game.ID, Elo: uint32(1000 + i*100)})
	}

	best, err := repo.BestPlayers(ctx, 3, "")
	assert.NoError(t, err)
	assert.Len(t, best, 3)
	assert.Equal(t, uint32(1500), best[0].Elo)
	assert.Equal(t, uint32(1400), best[1].Elo)
	assert.Equal(t, uint32(1300), best[2].Elo)

	bestCached, err := repo.BestPlayers(ctx, 2, "")
	assert.NoError(t, err)
	assert.Len(t, bestCached, 2)
}

func TestUserRepository_BestPlayers_FiltersByGame(t *testing.T) {
	t.Parallel()
	database := testutil.SetupTestDB(t)
	repo := repository.NewUserRepository(database)
	ctx := context.Background()

	poker := &db.Game{Name: "Poker"}
	uno := &db.Game{Name: "Uno"}
	require.NoError(t, database.Create(poker).Error)
	require.NoError(t, database.Create(uno).Error)

	alice := &db.User{Username: "alice"}
	bob := &db.User{Username: "bob"}
	require.NoError(t, database.Create(alice).Error)
	require.NoError(t, database.Create(bob).Error)
	require.NoError(t, database.Create(&db.Ranking{UserID: alice.ID, GameID: poker.ID, Elo: 1800}).Error)
	require.NoError(t, database.Create(&db.Ranking{UserID: bob.ID, GameID: uno.ID, Elo: 1900}).Error)

	unoOnly, err := repo.BestPlayers(ctx, 10, "Uno")
	require.NoError(t, err)
	require.Len(t, unoOnly, 1)
	assert.Equal(t, "bob", unoOnly[0].User.Username)

	all, err := repo.BestPlayers(ctx, 10, "")
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Equal(t, "bob", all[0].User.Username, "highest Elo across games wins the mixed board")

	missing, err := repo.BestPlayers(ctx, 10, "Hearts")
	require.NoError(t, err)
	assert.Empty(t, missing)
}

func TestUserRepository_UserProfile(t *testing.T) {
	t.Parallel()
	database := testutil.SetupTestDB(t)
	repo := repository.NewUserRepository(database)
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
	database := testutil.SetupTestDB(t)
	repo := repository.NewUserRepository(database)
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
