package repository

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/Pieczasz/terminal-card/internal/db"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
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

func (q *gormUserRepository) LoadUserByFingerprint(
	ctx context.Context, fingerprint string,
) (_ *db.User, _ *db.PublicKey, err error) {
	ctx, span := tracer.Start(ctx, "db.LoadUserByFingerprint")
	defer func() { endSpan(span, err) }()

	var dbKey db.PublicKey
	err = q.db.WithContext(ctx).Where("fingerprint = ?", fingerprint).
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

	// The Preload applies deleted_at IS NULL while the public_keys row still matches, so
	// a soft-deleted account arrives as a zero-valued association rather than a miss -
	// handing that back authenticates the key as user 0.
	if dbKey.User.ID == 0 {
		return nil, nil, nil
	}
	return &dbKey.User, &dbKey, nil
}

func (q *gormUserRepository) RegisterUserWithKey(
	ctx context.Context, username, fingerprint string,
) (_ *db.User, _ *db.PublicKey, err error) {
	ctx, span := tracer.Start(ctx, "db.RegisterUserWithKey")
	defer func() { endSpan(span, err) }()

	if err := db.ValidateUsername(username); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrInvalidUsername, err)
	}

	var currentUser db.User
	var dbKey db.PublicKey

	err = q.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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

// Read lock only: the database query must never run while the mutex is held.
func (q *gormUserRepository) cachedBestPlayers(gameName string, limit int) ([]db.Ranking, bool) {
	q.bestPlayersCacheMutex.RLock()
	defer q.bestPlayersCacheMutex.RUnlock()

	entry, ok := q.bestPlayersCache[gameName]
	if !ok || time.Since(entry.at) >= bestPlayersCacheTTL {
		return nil, false
	}
	return slices.Clone(entry.rankings[:min(limit, len(entry.rankings))]), true
}

func (q *gormUserRepository) BestPlayers(
	ctx context.Context, limit int, gameName string,
) (_ []db.Ranking, err error) {
	ctx, span := tracer.Start(ctx, "db.BestPlayers", trace.WithAttributes(attribute.Int("limit", limit)))
	defer func() { endSpan(span, err) }()

	limit = max(limit, 0)

	// An entry holds at most bestPlayersCacheSize rows, so a larger ask cannot be served
	// from it and must not be stored into it: later callers would get a truncated board.
	cacheable := limit <= bestPlayersCacheSize
	if cacheable {
		if out, fresh := q.cachedBestPlayers(gameName, limit); fresh {
			return out, nil
		}
	}

	fetch := bestPlayersCacheSize
	if !cacheable {
		fetch = limit
	}

	query := q.db.WithContext(ctx).Preload("User").Preload("Game").
		Order("elo desc").
		Limit(fetch)
	if gameName != "" {
		query = query.Joins("JOIN games ON games.id = rankings.game_id AND games.deleted_at IS NULL").
			Where("games.name = ?", gameName)
	}

	var rankings []db.Ranking
	if err = query.Find(&rankings).Error; err != nil {
		return nil, fmt.Errorf("get best players: %w", err)
	}
	span.SetAttributes(attribute.Int("rows", len(rankings)))

	// Not caching an empty result is what bounds this map: gameName is caller-controlled,
	// and an unknown game returns no rows, so it never becomes a key.
	if cacheable && len(rankings) > 0 {
		at := time.Now()
		q.bestPlayersCacheMutex.Lock()
		if entry, ok := q.bestPlayersCache[gameName]; !ok || entry.at.Before(at) {
			q.bestPlayersCache[gameName] = bestPlayersCacheEntry{rankings: rankings, at: at}
		}
		q.bestPlayersCacheMutex.Unlock()
	}
	return slices.Clone(rankings[:min(limit, len(rankings))]), nil
}

func (q *gormUserRepository) UserProfile(ctx context.Context, userID uint) (_ *db.User, err error) {
	ctx, span := tracer.Start(ctx, "db.UserProfile",
		trace.WithAttributes(attribute.Int64("user_id", int64(userID))))
	defer func() { endSpan(span, err) }()

	var user db.User
	err = q.db.WithContext(ctx).Preload("PublicKeys").
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

// UpdateUserActivity stamps both last-seen fields; the caller decides whether a stale
// timestamp matters.
func (q *gormUserRepository) UpdateUserActivity(
	ctx context.Context, user *db.User, key *db.PublicKey,
) (err error) {
	ctx, span := tracer.Start(ctx, "db.UpdateUserActivity")
	defer func() { endSpan(span, err) }()

	if err = q.db.WithContext(ctx).Model(user).Update("LastSeenAt", time.Now()).Error; err != nil {
		return fmt.Errorf("update last seen: %w", err)
	}
	if err = q.db.WithContext(ctx).Model(key).Omit("User").Update("LastUsedAt", time.Now()).Error; err != nil {
		return fmt.Errorf("update key last used: %w", err)
	}
	return nil
}

func (q *gormUserRepository) UserMatchHistory(
	ctx context.Context, userID uint, limit int,
) (_ []db.MatchParticipant, err error) {
	ctx, span := tracer.Start(ctx, "db.UserMatchHistory",
		trace.WithAttributes(attribute.Int64("user_id", int64(userID)), attribute.Int("limit", limit)))
	defer func() { endSpan(span, err) }()

	// GORM reads a negative Limit as "no limit", which would stream the whole history.
	limit = max(limit, 0)

	var history []db.MatchParticipant
	err = q.db.WithContext(ctx).Where("user_id = ?", userID).
		Preload("Match").
		Preload("Match.Game").
		Order("match_id desc").
		Limit(limit).
		Find(&history).Error
	if err != nil {
		return nil, fmt.Errorf("get user match history: %w", err)
	}
	span.SetAttributes(attribute.Int("rows", len(history)))
	return history, nil
}
