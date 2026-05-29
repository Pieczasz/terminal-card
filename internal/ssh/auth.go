package ssh

import (
	"errors"
	"log/slog"

	"client/internal/db"

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

	user, err := queries.AuthenticateAndLoadUser(fingerprint, sshUsername)
	if err != nil {
		slog.Error("database error while authenticating user", "error", err)
		return nil, ErrInternal
	}

	return user, nil
}
