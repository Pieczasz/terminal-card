package db

import "errors"

// Auth/registration failures the SSH layer surfaces to the player. They live next
// to UserRepository so transport depends on the contract package, not the GORM
// implementation.
var (
	ErrUsernameTaken        = errors.New("username already taken, please choose another via ssh config")
	ErrKeyAlreadyRegistered = errors.New("public key already registered")
	ErrInvalidUsername      = errors.New("invalid username")
)
