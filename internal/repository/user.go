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
		return nil, nil, err
	}
	return &dbKey.User, &dbKey, nil
}

func (q *GormUserRepository) RegisterUserWithKey(ctx context.Context, username, fingerprint string) (*db.User, *db.PublicKey, error) {
	// TODO: move validate username to different package?
	if err := db.ValidateUsername(username); err != nil {
		return nil, nil, fmt.Errorf("invalid username: %w", err)
	}

	var existingUser db.User
	err := q.db.WithContext(ctx).Where("username = ?", username).First(&existingUser).Error
	if err == nil {
		return nil, nil, errors.New("username already taken, please choose another via ssh config")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, err
	}

	currentUser := db.User{
		Username:   username,
		LastSeenAt: time.Now(),
	}

	if err := q.db.WithContext(ctx).Create(&currentUser).Error; err != nil {
		return nil, nil, err
	}

	dbKey := db.PublicKey{
		Fingerprint: fingerprint,
		Name:        "auto-generated key",
		UserID:      currentUser.ID,
		LastUsedAt:  time.Now(),
	}

	if err := q.db.WithContext(ctx).Create(&dbKey).Error; err != nil {
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

	if err == nil {
		q.bestPlayersCache = rankings
		q.bestPlayersCacheTime = time.Now()
	}

	if len(rankings) > limit {
		rankings = rankings[:limit]
	}

	return rankings, err
}

func (q *GormUserRepository) GetUserProfile(ctx context.Context, userID uint) (*db.User, error) {
	var user db.User
	err := q.db.WithContext(ctx).Preload("PublicKeys").
		Preload("Rankings").
		Preload("Rankings.Game").
		First(&user, userID).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (q *GormUserRepository) UpdateUserActivity(ctx context.Context, user *db.User, key *db.PublicKey) {
	if err := q.db.WithContext(ctx).Model(user).Update("LastSeenAt", time.Now()).Error; err != nil {
		slog.Error("unexpected error while trying to update LastSeenAt field", "user", user, "error", err)
	}
	if err := q.db.WithContext(ctx).Model(key).Update("LastUsedAt", time.Now()).Error; err != nil {
		slog.Error("unexpected error while trying to update LastUsedAt field", "user", user, "error", err)
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
	return history, err
}
