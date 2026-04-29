package ssh

import (
	"errors"
	"log"
	"time"

	"client/internal/db"

	"github.com/charmbracelet/ssh"
	cryptossh "golang.org/x/crypto/ssh"
	"gorm.io/gorm"
)

var (
	// ErrNoPublicKey is returned when the SSH session does not provide a public key.
	ErrNoPublicKey = errors.New("SSH key authentication is required")
	// ErrInternal is a generic error returned to the user to avoid leaking sensitive information.
	ErrInternal = errors.New("internal server error")
)

// AuthenticateAndLoadUser authenticates the user based on their SSH public key.
// It creates a new user and key record if they do not exist or updates their last seen timestamps.
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
				log.Printf("failed to create user: %v", err)
				return nil, ErrInternal
			}

			dbKey = db.PublicKey{
				Fingerprint: fingerprint,
				Name:        "Auto-generated Key",
				UserID:      currentUser.ID,
				LastUsedAt:  time.Now(),
			}

			if err := database.Create(&dbKey).Error; err != nil {
				log.Printf("failed to create public key: %v", err)
				return nil, ErrInternal
			}
		} else {
			log.Printf("database error while retrieving key: %v", err)
			return nil, ErrInternal
		}
	} else {
		currentUser = dbKey.User

		if err := database.Model(&currentUser).Update("LastSeenAt", time.Now()).Error; err != nil {
			log.Printf("failed to update user last seen timestamp: %v", err)
		}
		if err := database.Model(&dbKey).Update("LastUsedAt", time.Now()).Error; err != nil {
			log.Printf("failed to update key last used timestamp: %v", err)
		}
	}

	return &currentUser, nil
}
