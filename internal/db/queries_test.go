package db

import (
	"terminalcard/internal/testutil"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestQueries_RegisterUserWithKey(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t, &User{}, &PublicKey{}, &Ranking{})
	queries := NewQueries(db)

	t.Run("successful registration", func(t *testing.T) {
		user, key, err := queries.RegisterUserWithKey("player_one", "fingerprint_1")
		assert.NoError(t, err)
		assert.NotNil(t, user)
		assert.Equal(t, "player_one", user.Username)
		assert.Equal(t, "fingerprint_1", key.Fingerprint)
	})

	t.Run("username already taken", func(t *testing.T) {
		_, _, err := queries.RegisterUserWithKey("player_one", "fingerprint_2")
		assert.ErrorContains(t, err, "username already taken")
	})

	t.Run("invalid username length", func(t *testing.T) {
		_, _, err := queries.RegisterUserWithKey("this_username_is_way_too_long", "fingerprint_3")
		assert.ErrorContains(t, err, "username cannot exceed 16 characters")
	})
}

func TestQueries_LoadUserByFingerprint(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t, &User{}, &PublicKey{}, &Ranking{})
	queries := NewQueries(db)

	_, _, err := queries.RegisterUserWithKey("player_two", "fingerprint_abc")
	assert.NoError(t, err)

	t.Run("existing user", func(t *testing.T) {
		user, key, err := queries.LoadUserByFingerprint("fingerprint_abc")
		assert.NoError(t, err)
		assert.NotNil(t, user)
		assert.NotNil(t, key)
		assert.Equal(t, "player_two", user.Username)
	})

	t.Run("non-existent user", func(t *testing.T) {
		user, key, err := queries.LoadUserByFingerprint("fingerprint_unknown")
		assert.NoError(t, err)
		assert.Nil(t, user)
		assert.Nil(t, key)
	})
}

func TestQueries_UpdateUserActivity(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t, &User{}, &PublicKey{}, &Ranking{})
	queries := NewQueries(db)

	user, key, err := queries.RegisterUserWithKey("player_three", "fingerprint_xyz")
	assert.NoError(t, err)

	initialSeenAt := user.LastSeenAt
	initialUsedAt := key.LastUsedAt

	queries.UpdateUserActivity(user, key)

	updatedUser, updatedKey, err := queries.LoadUserByFingerprint("fingerprint_xyz")
	assert.NoError(t, err)

	assert.True(t, updatedUser.LastSeenAt.After(initialSeenAt) || updatedUser.LastSeenAt.Equal(initialSeenAt))
	assert.True(t, updatedKey.LastUsedAt.After(initialUsedAt) || updatedKey.LastUsedAt.Equal(initialUsedAt))
}
