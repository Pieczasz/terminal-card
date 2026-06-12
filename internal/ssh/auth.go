package ssh

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"terminalcard/internal/db"

	"github.com/charmbracelet/ssh"
	cryptossh "golang.org/x/crypto/ssh"
)

var (
	ErrNoPublicKey = errors.New("SSH key authentication is required")
	ErrInternal    = errors.New("internal server error")
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
			return nil, fmt.Errorf("failed to register new user: %w", err)
		}
	} else {
		userRepo.UpdateUserActivity(ctx, user, key)
	}

	return user, nil
}
