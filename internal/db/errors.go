package db

import "errors"

// Auth/registration failures the SSH layer surfaces to the player. They live next
// to UserRepository so transport depends on the contract package, not the GORM
// implementation.
var (
	ErrUsernameTaken        = errors.New("username already taken, please choose another via ssh config")
	ErrKeyAlreadyRegistered = errors.New("public key already registered")
	ErrInvalidUsername      = errors.New("invalid username")

	// ErrUserNotFound is returned by an operation aimed at one account when no such
	// row exists. Erasure is the caller that needs to tell "nothing to do" apart from
	// "the delete failed", so it is a sentinel rather than a wrapped GORM error.
	ErrUserNotFound = errors.New("user not found")
)
