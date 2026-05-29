package db

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// Queries provides abstraction layer for database operations
type Queries struct {
	db *gorm.DB
}

func NewQueries(db *gorm.DB) *Queries {
	return &Queries{db: db}
}

// GetBestPlayers returns the top players across all games, or filtered by gameID.
// TODO: cache this, maybe like top 10 of each
func (q *Queries) GetBestPlayers(limit int) ([]Ranking, error) {
	var rankings []Ranking
	err := q.db.Preload("User").Preload("Game").
		Order("elo desc").
		Limit(limit).
		Find(&rankings).Error
	return rankings, err
}

// GetUserProfile retrieves a user by ID with their public keys and rankings.
func (q *Queries) GetUserProfile(userID uint) (*User, error) {
	var user User
	err := q.db.Preload("PublicKeys").
		Preload("Rankings").
		Preload("Rankings.Game").
		First(&user, userID).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// AuthenticateAndLoadUser loads a user by SSH key fingerprint, creating them if they don't exist.
func (q *Queries) AuthenticateAndLoadUser(fingerprint, sshUsername string) (*User, error) {
	var dbKey PublicKey
	var currentUser User

	err := q.db.Where("fingerprint = ?", fingerprint).Preload("User").First(&dbKey).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			currentUser = User{
				Username:   sshUsername,
				LastSeenAt: time.Now(),
			}

			if err := q.db.Create(&currentUser).Error; err != nil {
				return nil, err
			}

			dbKey = PublicKey{
				Fingerprint: fingerprint,
				Name:        "Auto-generated Key",
				UserID:      currentUser.ID,
				LastUsedAt:  time.Now(),
			}

			if err := q.db.Create(&dbKey).Error; err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	} else {
		currentUser = dbKey.User

		if err := q.db.Model(&currentUser).Update("LastSeenAt", time.Now()).Error; err != nil {
			// Ignore update errors for last seen
		}
		if err := q.db.Model(&dbKey).Update("LastUsedAt", time.Now()).Error; err != nil {
			// Ignore update errors for last seen
		}
	}

	return &currentUser, nil
}
