package db

import (
	"errors"
	"log/slog"
	"regexp"
	"time"

	"gorm.io/gorm"
)

var usernameRegex = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

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

func (q *Queries) LoadUserByFingerprint(fingerprint string) (*User, *PublicKey, error) {
	var dbKey PublicKey
	err := q.db.Where("fingerprint = ?", fingerprint).Preload("User").First(&dbKey).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	return &dbKey.User, &dbKey, nil
}

func (q *Queries) RegisterUserWithKey(username, fingerprint string) (*User, *PublicKey, error) {
	if len(username) > 16 {
		return nil, nil, errors.New("Username cannot exceed 16 characters")
	}
	if !usernameRegex.MatchString(username) {
		return nil, nil, errors.New("Username can only contain English letters, numbers, and underscores")
	}

	var existingUser User
	err := q.db.Where("username = ?", username).First(&existingUser).Error
	if err == nil {
		return nil, nil, errors.New("Username already taken, please choose another via ssh config")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, err
	}

	currentUser := User{
		Username:   username,
		LastSeenAt: time.Now(),
	}

	if err := q.db.Create(&currentUser).Error; err != nil {
		return nil, nil, err
	}

	dbKey := PublicKey{
		Fingerprint: fingerprint,
		Name:        "Auto-generated Key",
		UserID:      currentUser.ID,
		LastUsedAt:  time.Now(),
	}

	if err := q.db.Create(&dbKey).Error; err != nil {
		return nil, nil, err
	}

	return &currentUser, &dbKey, nil
}

func (q *Queries) UpdateUserActivity(user *User, key *PublicKey) {
	if err := q.db.Model(user).Update("LastSeenAt", time.Now()).Error; err != nil {
		slog.Error("unexpected error while trying to update LastSeenAt field", "user", user, "error", err)
	}
	if err := q.db.Model(key).Update("LastUsedAt", time.Now()).Error; err != nil {
		slog.Error("unexpected error while trying to update LastUsedAt field", "user", user, "error", err)
	}
}
