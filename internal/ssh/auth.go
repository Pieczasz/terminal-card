package ssh

import (
	"context"
	"errors"
	"log/slog"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/repository"

	"charm.land/ssh"
	cryptossh "golang.org/x/crypto/ssh"
)

var (
	ErrNoPublicKey        = errors.New("SSH key authentication is required")
	ErrInternal           = errors.New("internal server error")
	ErrRegistrationFailed = errors.New("registration failed")
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

// mapRegisterError passes through the registration failures a user can act on and
// collapses everything else into a generic failure. The repository sentinels are
// returned unchanged rather than translated into ssh-local copies, so errors.Is
// still matches at the caller and the underlying cause survives for logging.
func mapRegisterError(err error) error {
	switch {
	case errors.Is(err, repository.ErrUsernameTaken),
		errors.Is(err, repository.ErrInvalidUsername),
		errors.Is(err, repository.ErrKeyAlreadyRegistered):
		return err
	default:
		return ErrRegistrationFailed
	}
}
