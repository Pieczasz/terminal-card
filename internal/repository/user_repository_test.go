package repository

import (
	"context"
	"testing"

	"terminalcard/internal/db"
	"terminalcard/internal/testutil"

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
