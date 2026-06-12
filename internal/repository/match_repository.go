package repository

import (
	"context"
	"fmt"
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

func (q *GormMatchRepository) UpdateRankings(ctx context.Context, gameID uint, orderedUserIDs []uint) (map[uint]int, error) {
	if len(orderedUserIDs) == 0 {
		return nil, nil
	}

	deltas := make(map[uint]int)

	if err := q.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rankingMap, err := q.fetchRankings(tx, gameID, orderedUserIDs)
		if err != nil {
			return err
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
					Elo:    uint32(elo.DefaultRating),
				}
			}

			oldRating := float64(r.Elo)
			if !exists {
				oldRating = elo.DefaultRating
			}

			r.Elo = uint32(newRating)
			if err := tx.Save(r).Error; err != nil {
				return err
			}

			deltas[userID] = int(newRating) - int(oldRating)
		}
		return nil
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
		match, err := q.recordNewMatch(tx, gameID)
		if err != nil {
			return err
		}

		for i, userID := range orderedUserIDs {
			delta := eloDeltas[userID]

			participant := db.MatchParticipant{
				MatchID:   match.ID,
				UserID:    userID,
				Placement: i + 1,
				EloDelta:  delta,
			}
			if err := tx.Create(&participant).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to record match transaction: %w", err)
	}
	return nil
}

func (q *GormMatchRepository) fetchRankings(tx *gorm.DB, gameID uint, userIDs []uint) (map[uint]*db.Ranking, error) {
	var rankings []db.Ranking
	if err := tx.Where("user_id IN ? AND game_id = ?", userIDs, gameID).Find(&rankings).Error; err != nil {
		return nil, err
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
		return nil, err
	}
	return &match, nil
}
