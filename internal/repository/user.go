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

const (
	pgUniqueViolationCode = "23505"
	bestPlayersCacheSize  = 200
	bestPlayersCacheTTL   = 5 * time.Minute
)

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolationCode
}

type bestPlayersCacheEntry struct {
	rankings []db.Ranking
	at       time.Time
}

type gormUserRepository struct {
	db                    *gorm.DB
	bestPlayersCache      map[string]bestPlayersCacheEntry
	bestPlayersCacheMutex sync.RWMutex
}

func NewUserRepository(database *gorm.DB) db.UserRepository {
	return &gormUserRepository{
		db:               database,
		bestPlayersCache: make(map[string]bestPlayersCacheEntry),
	}
}

func (q *gormUserRepository) LoadUserByFingerprint(ctx context.Context, fingerprint string) (*db.User, *db.PublicKey, error) {
	ctx, span := tracer.Start(ctx, "db.LoadUserByFingerprint")
	defer span.End()

	var dbKey db.PublicKey
	err := q.db.WithContext(ctx).Where("fingerprint = ?", fingerprint).
		Preload("User").
		Preload("User.Rankings").
		Preload("User.Rankings.Game").
		First(&dbKey).Error
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

func (q *gormUserRepository) BestPlayers(ctx context.Context, limit int, gameName string) ([]db.Ranking, error) {
	ctx, span := tracer.Start(ctx, "db.BestPlayers")
	defer span.End()

	limit = max(limit, 0)

	cached := func() ([]db.Ranking, bool) {
		entry, ok := q.bestPlayersCache[gameName]
		if !ok || time.Since(entry.at) >= bestPlayersCacheTTL {
			return nil, false
		}
		return slices.Clone(entry.rankings[:min(limit, len(entry.rankings))]), true
	}

	q.bestPlayersCacheMutex.RLock()
	out, fresh := cached()
	q.bestPlayersCacheMutex.RUnlock()
	if fresh {
		return out, nil
	}

	q.bestPlayersCacheMutex.Lock()
	defer q.bestPlayersCacheMutex.Unlock()
	if out, fresh := cached(); fresh {
		return out, nil
	}

	query := q.db.WithContext(ctx).Preload("User").Preload("Game").
		Order("elo desc").
		Limit(bestPlayersCacheSize)
	if gameName != "" {
		query = query.Joins("JOIN games ON games.id = rankings.game_id AND games.deleted_at IS NULL").
			Where("games.name = ?", gameName)
	}

	var rankings []db.Ranking
	if err := query.Find(&rankings).Error; err != nil {
		return nil, fmt.Errorf("get best players: %w", err)
	}

	if len(rankings) > 0 {
		q.bestPlayersCache[gameName] = bestPlayersCacheEntry{rankings: rankings, at: time.Now()}
	}
	return slices.Clone(rankings[:min(limit, len(rankings))]), nil
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
	if err := q.db.WithContext(ctx).Model(key).Omit("User").Update("LastUsedAt", time.Now()).Error; err != nil {
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
		Order("match_id desc").
		Limit(limit).
		Find(&history).Error
	if err != nil {
		return nil, fmt.Errorf("get user match history: %w", err)
	}
	return history, nil
}
