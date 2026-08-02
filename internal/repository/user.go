package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/Pieczasz/terminal-card/internal/db"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

var (
	ErrUsernameTaken        = errors.New("username already taken, please choose another via ssh config")
	ErrKeyAlreadyRegistered = errors.New("public key already registered")
	ErrInvalidUsername      = errors.New("invalid username")
	ErrUserNotFound         = errors.New("user not found")
)

// pgUniqueViolationCode is the Postgres SQLSTATE for a unique constraint violation.
const pgUniqueViolationCode = "23505"

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolationCode
}

type gormUserRepository struct {
	db                    *gorm.DB
	bestPlayersCache      []db.Ranking
	bestPlayersCacheTime  time.Time
	bestPlayersCacheMutex sync.RWMutex
}

func NewUserRepository(db *gorm.DB) db.UserRepository {
	return &gormUserRepository{db: db}
}

func (q *gormUserRepository) LoadUserByFingerprint(ctx context.Context, fingerprint string) (*db.User, *db.PublicKey, error) {
	ctx, span := tracer.Start(ctx, "db.LoadUserByFingerprint")
	defer span.End()

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

func (q *gormUserRepository) RegisterUserWithKey(ctx context.Context, username, fingerprint string) (*db.User, *db.PublicKey, error) {
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
			if isUniqueViolation(err) { // lost a concurrent registration race
				return ErrUsernameTaken
			}
			return fmt.Errorf("create user: %w", err)
		}

		dbKey = db.PublicKey{
			Fingerprint: fingerprint,
			Name:        "auto-generated key",
			UserID:      currentUser.ID,
			LastUsedAt:  time.Now(),
		}
		if err := tx.Create(&dbKey).Error; err != nil {
			if isUniqueViolation(err) {
				return ErrKeyAlreadyRegistered
			}
			return fmt.Errorf("create public key: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("register user transaction: %w", err)
	}

	return &currentUser, &dbKey, nil
}

func (q *gormUserRepository) BestPlayers(ctx context.Context, limit int) ([]db.Ranking, error) {
	ctx, span := tracer.Start(ctx, "db.BestPlayers")
	defer span.End()

	q.bestPlayersCacheMutex.RLock()
	if time.Since(q.bestPlayersCacheTime) < 5*time.Minute && len(q.bestPlayersCache) >= limit {
		cacheCopy := slices.Clone(q.bestPlayersCache[:limit])
		q.bestPlayersCacheMutex.RUnlock()
		return cacheCopy, nil
	}
	q.bestPlayersCacheMutex.RUnlock()

	q.bestPlayersCacheMutex.Lock()
	defer q.bestPlayersCacheMutex.Unlock()
	if time.Since(q.bestPlayersCacheTime) < 5*time.Minute && len(q.bestPlayersCache) >= limit {
		cacheCopy := slices.Clone(q.bestPlayersCache[:limit])
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
		out := slices.Clone(rankings[:limit])
		return out, nil
	}

	out := slices.Clone(rankings)
	return out, nil
}

func (q *gormUserRepository) UserProfile(ctx context.Context, userID uint) (*db.User, error) {
	ctx, span := tracer.Start(ctx, "db.UserProfile")
	defer span.End()

	var user db.User
	err := q.db.WithContext(ctx).Preload("PublicKeys").
		Preload("Rankings").
		Preload("Rankings.Game").
		First(&user, userID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("get user profile: %w", err)
	}
	return &user, nil
}

func (q *gormUserRepository) UpdateUserActivity(ctx context.Context, user *db.User, key *db.PublicKey) {
	if err := q.db.WithContext(ctx).Model(user).Update("LastSeenAt", time.Now()).Error; err != nil {
		slog.Error("unexpected error while trying to update LastSeenAt field", "user_id", user.ID, "error", err)
	}
	if err := q.db.WithContext(ctx).Model(key).Update("LastUsedAt", time.Now()).Error; err != nil {
		slog.Error("unexpected error while trying to update LastUsedAt field", "user_id", user.ID, "error", err)
	}
}

func (q *gormUserRepository) UserMatchHistory(ctx context.Context, userID uint, limit int) ([]db.MatchParticipant, error) {
	ctx, span := tracer.Start(ctx, "db.UserMatchHistory")
	defer span.End()

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
