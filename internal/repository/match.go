package repository

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"terminalcard/internal/db"
	"terminalcard/internal/elo"

	"gorm.io/gorm"
)

type GormMatchRepository struct {
	db *gorm.DB
}

func NewMatchRepository(db *gorm.DB) db.MatchRepository {
	return &GormMatchRepository{db: db}
}

func (q *GormMatchRepository) GetOrCreateGame(ctx context.Context, name string) (*db.Game, error) {
	var game db.Game
	err := q.db.WithContext(ctx).Where("name = ?", name).FirstOrCreate(&game, db.Game{Name: name}).Error
	if err != nil {
		return nil, fmt.Errorf("get or create game: %w", err)
	}
	return &game, nil
}

func (q *GormMatchRepository) UpdateRankings(ctx context.Context, gameID uint, orderedUserIDs []uint) (map[uint]int, error) {
	if len(orderedUserIDs) == 0 {
		return nil, nil
	}

	var deltas map[uint]int
	if err := q.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		deltas, err = q.updateRankingsTx(tx, gameID, orderedUserIDs)
		return err
	}); err != nil {
		return nil, fmt.Errorf("failed to update rankings transaction: %w", err)
	}

	return deltas, nil
}

func (q *GormMatchRepository) RecordMatch(ctx context.Context, gameID uint, orderedUserIDs []uint, eloDeltas map[uint]int) error {
	if len(orderedUserIDs) == 0 {
		return nil
	}

	if err := q.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return q.recordMatchTx(tx, gameID, orderedUserIDs, eloDeltas)
	}); err != nil {
		return fmt.Errorf("failed to record match transaction: %w", err)
	}
	return nil
}

// FinalizeRankedMatch creates/looks up the game, updates rankings, and records the match
// in a single database transaction so ELO and history cannot diverge.
func (q *GormMatchRepository) FinalizeRankedMatch(ctx context.Context, gameName string, orderedUserIDs []uint) error {
	if len(orderedUserIDs) == 0 {
		return nil
	}

	if err := q.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var game db.Game
		if err := tx.Where("name = ?", gameName).FirstOrCreate(&game, db.Game{Name: gameName}).Error; err != nil {
			return fmt.Errorf("get or create game: %w", err)
		}

		deltas, err := q.updateRankingsTx(tx, game.ID, orderedUserIDs)
		if err != nil {
			return err
		}
		return q.recordMatchTx(tx, game.ID, orderedUserIDs, deltas)
	}); err != nil {
		return fmt.Errorf("finalize ranked match: %w", err)
	}
	return nil
}

func (q *GormMatchRepository) updateRankingsTx(tx *gorm.DB, gameID uint, orderedUserIDs []uint) (map[uint]int, error) {
	deltas := make(map[uint]int)

	rankingMap, err := q.fetchRankings(tx, gameID, orderedUserIDs)
	if err != nil {
		return nil, fmt.Errorf("fetch rankings: %w", err)
	}

	newRatings := q.calculateNewElos(orderedUserIDs, rankingMap)

	for _, userID := range orderedUserIDs {
		userIDStr := strconv.FormatUint(uint64(userID), 10)
		newRating := newRatings[userIDStr]

		r, exists := rankingMap[userID]
		if !exists {
			r = &db.Ranking{
				UserID: userID,
				GameID: gameID,
				Elo:    elo.ToUint32(elo.DefaultRating),
			}
		}

		oldRating := float64(r.Elo)
		if !exists {
			oldRating = elo.DefaultRating
		}

		stored := elo.ToUint32(newRating)
		r.Elo = stored
		if !exists {
			if err := tx.Create(r).Error; err != nil {
				return nil, fmt.Errorf("create ranking: %w", err)
			}
		} else {
			if err := tx.Model(r).Update("elo", stored).Error; err != nil {
				return nil, fmt.Errorf("update ranking: %w", err)
			}
		}

		deltas[userID] = int(stored) - int(math.Round(oldRating))
	}
	return deltas, nil
}

func (q *GormMatchRepository) recordMatchTx(tx *gorm.DB, gameID uint, orderedUserIDs []uint, eloDeltas map[uint]int) error {
	match, err := q.recordNewMatch(tx, gameID)
	if err != nil {
		return err
	}

	for i, userID := range orderedUserIDs {
		participant := db.MatchParticipant{
			MatchID:   match.ID,
			UserID:    userID,
			Placement: i + 1,
			EloDelta:  eloDeltas[userID],
		}
		if err := tx.Create(&participant).Error; err != nil {
			return fmt.Errorf("create match participant: %w", err)
		}
	}
	return nil
}

func (q *GormMatchRepository) fetchRankings(tx *gorm.DB, gameID uint, userIDs []uint) (map[uint]*db.Ranking, error) {
	var rankings []db.Ranking
	if err := tx.Where("user_id IN ? AND game_id = ?", userIDs, gameID).Find(&rankings).Error; err != nil {
		return nil, fmt.Errorf("query rankings: %w", err)
	}

	rankingMap := make(map[uint]*db.Ranking)
	for i := range rankings {
		rankingMap[rankings[i].UserID] = &rankings[i]
	}
	return rankingMap, nil
}

func (q *GormMatchRepository) calculateNewElos(orderedUserIDs []uint, rankingMap map[uint]*db.Ranking) map[string]float64 {
	var players []elo.Player
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
	return elo.Calculate(players)
}

func (q *GormMatchRepository) recordNewMatch(tx *gorm.DB, gameID uint) (*db.Match, error) {
	match := db.Match{GameID: gameID}
	if err := tx.Create(&match).Error; err != nil {
		return nil, fmt.Errorf("create match: %w", err)
	}
	return &match, nil
}
