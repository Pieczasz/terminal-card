package db

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"terminalcard/internal/elo"

	"gorm.io/gorm"
)

// Queries provides abstraction layer for database operations.
type Queries struct {
	db *gorm.DB
}

func NewQueries(db *gorm.DB) *Queries {
	return &Queries{db: db}
}

// GetBestPlayers returns the top players across all games, or filtered by gameID.
// TODO: cache this, maybe like top 10 of each.
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
	if err := ValidateUsername(username); err != nil {
		return nil, nil, err
	}

	var existingUser User
	err := q.db.Where("username = ?", username).First(&existingUser).Error
	if err == nil {
		return nil, nil, errors.New("username already taken, please choose another via ssh config")
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
		Name:        "auto-generated key",
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

// UpdateGameRankings updates the rankings for a given game ID based on the standings.
// orderedUserIDs must be sorted from 1st place to last place.
func (q *Queries) UpdateGameRankings(gameID uint, orderedUserIDs []uint) error {
	if len(orderedUserIDs) == 0 {
		return nil
	}

	if err := q.db.Transaction(func(tx *gorm.DB) error {
		var players []elo.Player

		var rankings []Ranking
		if err := tx.Where("user_id IN ? AND game_id = ?", orderedUserIDs, gameID).Find(&rankings).Error; err != nil {
			return err
		}

		rankingMap := make(map[uint]*Ranking)
		for i := range rankings {
			rankingMap[rankings[i].UserID] = &rankings[i]
		}

		for _, userID := range orderedUserIDs {
			rating := elo.DefaultRating
			if r, ok := rankingMap[userID]; ok {
				rating = float64(r.Elo)
			}
			players = append(players, elo.Player{
				ID:     strconv.FormatUint(uint64(userID), 10),
				Rating: rating,
			})
		}

		newRatings := elo.Calculate(players)

		for userIDStr, newRating := range newRatings {
			userID, _ := strconv.ParseUint(userIDStr, 10, 64)
			uid := uint(userID)

			r, exists := rankingMap[uid]
			if !exists {
				r = &Ranking{
					UserID: uid,
					GameID: gameID,
				}
			}
			r.Elo = uint32(newRating)

			if err := tx.Save(r).Error; err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to update rankings transaction: %w", err)
	}
	return nil
}
