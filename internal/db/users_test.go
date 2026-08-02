package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
