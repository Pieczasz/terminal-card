package ssh

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"terminalcard/internal/db"
	"terminalcard/internal/repository"

	"github.com/charmbracelet/ssh"
	cryptossh "golang.org/x/crypto/ssh"
)

var (
	ErrNoPublicKey      = errors.New("SSH key authentication is required")
	ErrInternal         = errors.New("internal server error")
	ErrUsernameTaken    = errors.New("username already taken, please choose another via ssh config")
	ErrInvalidUsername  = errors.New("invalid username")
	ErrKeyAlreadyUsed   = errors.New("public key already registered")
	ErrRegistrationFail = errors.New("registration failed")
)

func AuthenticateSession(s ssh.Session) (string, error) {
	publicKey := s.PublicKey()
	if publicKey == nil {
		return "", ErrNoPublicKey
	}
	return cryptossh.FingerprintSHA256(publicKey), nil
}

func LoadOrRegisterUser(ctx context.Context, userRepo db.UserRepository, sshUsername, fingerprint string) (*db.User, error) {
	user, key, err := userRepo.LoadUserByFingerprint(ctx, fingerprint)
	if err != nil {
		slog.Error("database error while authenticating user", "error", err)
		return nil, ErrInternal
	}

	if user == nil {
		user, _, err = userRepo.RegisterUserWithKey(ctx, sshUsername, fingerprint)
		if err != nil {
			slog.Error("failed to register new user", "error", err)
			return nil, mapRegisterError(err)
		}
	} else {
		userRepo.UpdateUserActivity(ctx, user, key)
	}

	return user, nil
}

func mapRegisterError(err error) error {
	switch {
	case errors.Is(err, repository.ErrUsernameTaken):
		return ErrUsernameTaken
	case errors.Is(err, repository.ErrInvalidUsername):
		return ErrInvalidUsername
	case errors.Is(err, repository.ErrKeyAlreadyRegistered):
		return ErrKeyAlreadyUsed
	case strings.Contains(err.Error(), "invalid username"):
		return ErrInvalidUsername
	default:
		return ErrRegistrationFail
	}
}
