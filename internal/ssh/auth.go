package ssh

import (
	"errors"
	"log/slog"
	"time"

	"client/internal/db"

	"github.com/charmbracelet/ssh"
	cryptossh "golang.org/x/crypto/ssh"
	"gorm.io/gorm"
)

var (
	ErrNoPublicKey = errors.New("SSH key authentication is required")
	ErrInternal    = errors.New("internal server error")
)

func AuthenticateAndLoadUser(database *gorm.DB, s ssh.Session) (*db.User, error) {
	publicKey := s.PublicKey()
	if publicKey == nil {
		return nil, ErrNoPublicKey
	}

	fingerprint := cryptossh.FingerprintSHA256(publicKey)
	sshUsername := s.User()

	var dbKey db.PublicKey
	var currentUser db.User

	err := database.Where("fingerprint = ?", fingerprint).Preload("User").First(&dbKey).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			currentUser = db.User{
				Username:   sshUsername,
				LastSeenAt: time.Now(),
			}

			if err := database.Create(&currentUser).Error; err != nil {
				slog.Error("failed to create user", "error", err)
				return nil, ErrInternal
			}

			dbKey = db.PublicKey{
				Fingerprint: fingerprint,
				Name:        "Auto-generated Key",
				UserID:      currentUser.ID,
				LastUsedAt:  time.Now(),
			}

			if err := database.Create(&dbKey).Error; err != nil {
				slog.Error("failed to create public key", "error", err)
				return nil, ErrInternal
			}
		} else {
			slog.Error("database error while retrieving key", "error", err)
			return nil, ErrInternal
		}
	} else {
		currentUser = dbKey.User

		if err := database.Model(&currentUser).Update("LastSeenAt", time.Now()).Error; err != nil {
			slog.Error("failed to update user last seen timestamp", "error", err)
		}
		if err := database.Model(&dbKey).Update("LastUsedAt", time.Now()).Error; err != nil {
			slog.Error("failed to update key last used timestamp", "error", err)
		}
	}

	return &currentUser, nil
}
