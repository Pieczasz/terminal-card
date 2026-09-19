package db

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func TestValidateUsername_Valid(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		gen := rapid.StringMatching(`^[A-Za-z0-9_]{1,16}$`)
		username := gen.Draw(t, "username")

		err := ValidateUsername(username)
		assert.NoError(t, err)
	})
}

func TestValidateUsername_InvalidLength(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		gen := rapid.StringMatching(`^.{17,50}$`)
		username := gen.Draw(t, "username")

		err := ValidateUsername(username)
		assert.ErrorContains(t, err, "username cannot exceed 16 characters")
	})
}

func TestValidateUsername_InvalidCharacters(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		gen := rapid.StringMatching(`^.*[^A-Za-z0-9_].*$`).Filter(func(s string) bool {
			return len(s) > 0 && len(s) <= 16
		})
		username := gen.Draw(t, "username")

		err := ValidateUsername(username)
		assert.ErrorContains(t, err, "username can only contain English letters, numbers, and underscores")
	})
}

// FuzzValidateUsername covers a trust boundary: the username comes straight from the
// SSH client via s.User(). Validation must never be looser than the column it feeds
// (varchar(16), letters/digits/underscore), or the insert fails at the database
// instead of being refused with a clear message.
func FuzzValidateUsername(f *testing.F) {
	f.Add("alice")
	f.Add("")
	f.Add(strings.Repeat("a", 17))
	f.Add("a\x00b")
	f.Add("аdmin") // Cyrillic 'а'
	f.Add("日本語")
	f.Add("has space")
	f.Add("dash-not-allowed")

	f.Fuzz(func(t *testing.T, name string) {
		if ValidateUsername(name) != nil {
			return // rejected: nothing more to prove
		}
		assert.NotEmpty(t, name, "an accepted username must not be empty")
		assert.LessOrEqual(t, len(name), 16, "an accepted username must fit varchar(16)")
		assert.True(t, utf8.ValidString(name), "an accepted username must be valid UTF-8")
		for _, r := range name {
			isAllowed := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
				(r >= '0' && r <= '9') || r == '_'
			assert.True(t, isAllowed, "accepted username contains disallowed rune %q", r)
		}
	})
}

// The anonymised name is written into users.username, so it has to clear the same
// two constraints a chosen name does - varchar(16) and ^[A-Za-z0-9_]+$. A name that
// does not is an account the player cannot erase.
func TestAnonymisedUsername(t *testing.T) {
	t.Parallel()

	for _, id := range []uint{0, 1, 42, 99_999_999, 100_000_000, 1 << 40} {
		name := AnonymisedUsername(id)
		require.NoError(t, ValidateUsername(name), "id %d produced %q", id, name)
		assert.True(t, strings.HasPrefix(name, "deleted_"), "id %d produced %q", id, name)
	}

	assert.Equal(t, "deleted_42", AnonymisedUsername(42), "a readable id stays readable")
	assert.Len(t, AnonymisedUsername(99_999_999), MaxUsernameLength, "the decimal form fills the column")

	// Distinct ids must stay distinct names, or two erased accounts collide on the
	// unique index and the second player cannot be erased at all.
	seen := make(map[string]uint, 3)
	for _, id := range []uint{99_999_999, 100_000_000, 100_000_001} {
		name := AnonymisedUsername(id)
		_, clash := seen[name]
		assert.False(t, clash, "ids %d and %d both anonymise to %q", seen[name], id, name)
		seen[name] = id
	}
}
