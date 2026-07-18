package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"terminalcard/internal/db"

	"gorm.io/gorm"
)

var (
	ErrUsernameTaken       = errors.New("username already taken, please choose another via ssh config")
	ErrKeyAlreadyRegistered = errors.New("public key already registered")
	ErrInvalidUsername     = errors.New("invalid username")
)

type GormUserRepository struct {
	db                    *gorm.DB
	bestPlayersCache      []db.Ranking
	bestPlayersCacheTime  time.Time
	bestPlayersCacheMutex sync.RWMutex
}

func NewUserRepository(db *gorm.DB) db.UserRepository {
	return &GormUserRepository{db: db}
}

func (q *GormUserRepository) LoadUserByFingerprint(ctx context.Context, fingerprint string) (*db.User, *db.PublicKey, error) {
	var dbKey db.PublicKey
	err := q.db.WithContext(ctx).Where("fingerprint = ?", fingerprint).Preload("User").First(&dbKey).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("load user by fingerprint: %w", err)
	}
	return &dbKey.User, &dbKey, nil
}

func (q *GormUserRepository) RegisterUserWithKey(ctx context.Context, username, fingerprint string) (*db.User, *db.PublicKey, error) {
	if err := db.ValidateUsername(username); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrInvalidUsername, err)
	}

	var currentUser db.User
	var dbKey db.PublicKey

	err := q.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existingUser db.User
		err := tx.Where("username = ?", username).First(&existingUser).Error
		if err == nil {
			return ErrUsernameTaken
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("check username: %w", err)
		}

		var existingKey db.PublicKey
		err = tx.Where("fingerprint = ?", fingerprint).First(&existingKey).Error
		if err == nil {
			return ErrKeyAlreadyRegistered
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("check fingerprint: %w", err)
		}

		currentUser = db.User{
			Username:   username,
			LastSeenAt: time.Now(),
		}
		if err := tx.Create(&currentUser).Error; err != nil {
			return fmt.Errorf("create user: %w", err)
		}

		dbKey = db.PublicKey{
			Fingerprint: fingerprint,
			Name:        "auto-generated key",
			UserID:      currentUser.ID,
			LastUsedAt:  time.Now(),
		}
		if err := tx.Create(&dbKey).Error; err != nil {
			return fmt.Errorf("create public key: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	return &currentUser, &dbKey, nil
}

func (q *GormUserRepository) GetBestPlayers(ctx context.Context, limit int) ([]db.Ranking, error) {
	q.bestPlayersCacheMutex.RLock()
	if time.Since(q.bestPlayersCacheTime) < 5*time.Minute && len(q.bestPlayersCache) >= limit {
		cacheCopy := make([]db.Ranking, limit)
		copy(cacheCopy, q.bestPlayersCache[:limit])
		q.bestPlayersCacheMutex.RUnlock()
		return cacheCopy, nil
	}
	q.bestPlayersCacheMutex.RUnlock()

	q.bestPlayersCacheMutex.Lock()
	defer q.bestPlayersCacheMutex.Unlock()
	if time.Since(q.bestPlayersCacheTime) < 5*time.Minute && len(q.bestPlayersCache) >= limit {
		cacheCopy := make([]db.Ranking, limit)
		copy(cacheCopy, q.bestPlayersCache[:limit])
		return cacheCopy, nil
	}

	var rankings []db.Ranking
	err := q.db.WithContext(ctx).Preload("User").Preload("Game").
		Order("elo desc").
		Limit(100).
		Find(&rankings).Error
	if err != nil {
		return nil, fmt.Errorf("get best players: %w", err)
	}

	q.bestPlayersCache = rankings
	q.bestPlayersCacheTime = time.Now()

	if len(rankings) > limit {
		out := make([]db.Ranking, limit)
		copy(out, rankings[:limit])
		return out, nil
	}

	out := make([]db.Ranking, len(rankings))
	copy(out, rankings)
	return out, nil
}

func (q *GormUserRepository) GetUserProfile(ctx context.Context, userID uint) (*db.User, error) {
	var user db.User
	err := q.db.WithContext(ctx).Preload("PublicKeys").
		Preload("Rankings").
		Preload("Rankings.Game").
		First(&user, userID).Error
	if err != nil {
		return nil, fmt.Errorf("get user profile: %w", err)
	}
	return &user, nil
}

func (q *GormUserRepository) UpdateUserActivity(ctx context.Context, user *db.User, key *db.PublicKey) {
	if err := q.db.WithContext(ctx).Model(user).Update("LastSeenAt", time.Now()).Error; err != nil {
		slog.Error("unexpected error while trying to update LastSeenAt field", "user_id", user.ID, "error", err)
	}
	if err := q.db.WithContext(ctx).Model(key).Update("LastUsedAt", time.Now()).Error; err != nil {
		slog.Error("unexpected error while trying to update LastUsedAt field", "user_id", user.ID, "error", err)
	}
}

func (q *GormUserRepository) GetUserMatchHistory(ctx context.Context, userID uint, limit int) ([]db.MatchParticipant, error) {
	var history []db.MatchParticipant
	err := q.db.WithContext(ctx).Where("user_id = ?", userID).
		Preload("Match").
		Preload("Match.Game").
		Preload("Match.Participants").
		Preload("Match.Participants.User").
		Order("match_id desc").
		Limit(limit).
		Find(&history).Error
	if err != nil {
		return nil, fmt.Errorf("get user match history: %w", err)
	}
	return history, nil
}
