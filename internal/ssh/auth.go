package ssh

import (
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

func AuthenticateAndLoadUser(queries *db.Queries, s ssh.Session) (*db.User, error) {
	publicKey := s.PublicKey()
	if publicKey == nil {
		return nil, ErrNoPublicKey
	}

	fingerprint := cryptossh.FingerprintSHA256(publicKey)
	sshUsername := s.User()

	user, key, err := queries.LoadUserByFingerprint(fingerprint)
	if err != nil {
		slog.Error("database error while authenticating user", "error", err)
		return nil, ErrInternal
	}

	if user == nil {
		user, _, err = queries.RegisterUserWithKey(sshUsername, fingerprint)
		if err != nil {
			slog.Error("failed to register new user", "error", err)
			return nil, fmt.Errorf("failed to register new user: %w", err)
		}
	} else {
		queries.UpdateUserActivity(user, key)
	}

	return user, nil
}
