package repository

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/elo"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var tracer = otel.Tracer("terminal-card/repository")

// endSpan closes a span, marking it failed when the operation returned an error.
// Recording the error without setting the status leaves a trace in which the one
// span that failed still reads as successful.
func endSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

// checkDistinctPlayers rejects a repeated seat. A duplicate would move that
// player's rating twice and then collide on the match_participants primary key,
// rolling back the whole match after the Elo work was already done.
func checkDistinctPlayers(userIDs []uint) error {
	seen := make(map[uint]struct{}, len(userIDs))
	for _, id := range userIDs {
		if _, dup := seen[id]; dup {
			return fmt.Errorf("duplicate user id %d in match standings", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

type gormMatchRepository struct {
	db *gorm.DB
}

func NewMatchRepository(db *gorm.DB) db.MatchRepository {
	return &gormMatchRepository{db: db}
}

// getOrCreateGame takes the handle rather than reaching for q.db, because callers
// inside a transaction must reuse its connection. Going back to the pool there holds
// one connection while waiting for a second, so DBMaxOpenConnections concurrent
// finalizes deadlock until they time out, and the game row would outlive a rollback.
func getOrCreateGame(tx *gorm.DB, name string) (*db.Game, error) {
	game := db.Game{Name: name}
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoNothing: true,
	}).Create(&game).Error; err != nil {
		return nil, fmt.Errorf("create game: %w", err)
	}
	if game.ID == 0 {
		if err := tx.Where("name = ?", name).First(&game).Error; err != nil {
			return nil, fmt.Errorf("load game: %w", err)
		}
	}
	return &game, nil
}

func (q *gormMatchRepository) RecordCasualMatch(
	ctx context.Context, gameName string, orderedUserIDs []uint,
) (err error) {
	if len(orderedUserIDs) == 0 {
		return nil
	}

	ctx, span := tracer.Start(ctx, "db.RecordCasualMatch",
		trace.WithAttributes(attribute.String("game", gameName), attribute.Int("players", len(orderedUserIDs))))
	defer func() { endSpan(span, err) }()

	if err = checkDistinctPlayers(orderedUserIDs); err != nil {
		return fmt.Errorf("record casual match: %w", err)
	}

	if err = q.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		game, err := getOrCreateGame(tx, gameName)
		if err != nil {
			return err
		}
		// A casual result carries no placements, so history records the finish order.
		return q.recordMatchTx(tx, game.ID, orderedUserIDs, nil, nil, false)
	}); err != nil {
		err = fmt.Errorf("record casual match: %w", err)
		return err
	}
	return nil
}

// FinalizeRankedMatch creates/looks up the game, updates rankings, and records the match
// in a single database transaction so ELO and history cannot diverge.
func (q *gormMatchRepository) FinalizeRankedMatch(
	ctx context.Context, gameName string, orderedUserIDs []uint, places []int,
) (err error) {
	if len(orderedUserIDs) == 0 {
		return nil
	}

	ctx, span := tracer.Start(ctx, "db.FinalizeRankedMatch",
		trace.WithAttributes(attribute.String("game", gameName), attribute.Int("players", len(orderedUserIDs))))
	defer func() { endSpan(span, err) }()

	if err = checkDistinctPlayers(orderedUserIDs); err != nil {
		return fmt.Errorf("finalize ranked match: %w", err)
	}

	if err = q.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		game, err := getOrCreateGame(tx, gameName)
		if err != nil {
			return err
		}

		deltas, err := q.updateRankingsTx(tx, game.ID, orderedUserIDs, places)
		if err != nil {
			return err
		}
		return q.recordMatchTx(tx, game.ID, orderedUserIDs, places, deltas, true)
	}); err != nil {
		err = fmt.Errorf("finalize ranked match: %w", err)
		return err
	}
	return nil
}

// seedRankingRows gives fetchRankings something to lock. A first-time
// (user_id, game_id) has no row, so FOR UPDATE locks nothing and two concurrent
// finalizes both insert: one hits 23505, its transaction rolls back and the match is
// lost. Seeding at the default rating first turns that into an ordinary row lock.
// Sorted by user_id so two overlapping seeds take the locks in the same order and
// cannot deadlock.
func seedRankingRows(tx *gorm.DB, gameID uint, userIDs []uint) error {
	seeds := make([]db.Ranking, 0, len(userIDs))
	for _, userID := range slices.Sorted(slices.Values(userIDs)) {
		seeds = append(seeds, db.Ranking{UserID: userID, GameID: gameID, Elo: elo.ToUint32(elo.DefaultRating)})
	}
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "game_id"}},
		DoNothing: true,
	}).Create(&seeds).Error; err != nil {
		return fmt.Errorf("seed rankings: %w", err)
	}
	return nil
}

func (q *gormMatchRepository) updateRankingsTx(
	tx *gorm.DB, gameID uint, orderedUserIDs []uint, places []int,
) (map[uint]int, error) {
	if err := seedRankingRows(tx, gameID, orderedUserIDs); err != nil {
		return nil, err
	}

	rankingMap, err := q.fetchRankings(tx, gameID, orderedUserIDs)
	if err != nil {
		return nil, fmt.Errorf("fetch rankings: %w", err)
	}

	newRatings := q.calculateNewElos(orderedUserIDs, places, rankingMap)

	deltas := make(map[uint]int, len(orderedUserIDs))
	for _, userID := range orderedUserIDs {
		// Every seat was just seeded, so a miss means a soft-deleted ranking row the
		// seed skipped and the select filtered out - not a fresh player.
		r, ok := rankingMap[userID]
		if !ok {
			return nil, fmt.Errorf("no ranking row for user %d in game %d", userID, gameID)
		}
		// A key mismatch between the Elo result and the standings used to fall through
		// to the zero value and silently store rating 100 (the elo floor).
		newRating, ok := newRatings[strconv.FormatUint(uint64(userID), 10)]
		if !ok {
			return nil, fmt.Errorf("no elo result for user %d", userID)
		}

		oldElo := r.Elo
		stored := elo.ToUint32(newRating)
		if err := tx.Model(r).Update("elo", stored).Error; err != nil {
			return nil, fmt.Errorf("update ranking: %w", err)
		}

		deltas[userID] = int(stored) - int(oldElo)
	}
	return deltas, nil
}

func (q *gormMatchRepository) recordMatchTx(
	tx *gorm.DB, gameID uint, orderedUserIDs []uint, places []int, eloDeltas map[uint]int, ranked bool,
) error {
	match, err := q.recordNewMatch(tx, gameID, ranked)
	if err != nil {
		return err
	}

	participants := make([]db.MatchParticipant, len(orderedUserIDs))
	for i, userID := range orderedUserIDs {
		participants[i] = db.MatchParticipant{
			MatchID:   match.ID,
			UserID:    userID,
			Placement: placeAt(places, i),
			EloDelta:  eloDeltas[userID],
		}
	}
	// One multi-row insert: a table is at most a handful of seats, but a round trip
	// each was a round trip each.
	if err := tx.Create(&participants).Error; err != nil {
		return fmt.Errorf("create match participants: %w", err)
	}
	return nil
}

func (q *gormMatchRepository) fetchRankings(tx *gorm.DB, gameID uint, userIDs []uint) (map[uint]*db.Ranking, error) {
	var rankings []db.Ranking
	// FOR UPDATE: serialize concurrent finalize transactions to avoid lost Elo updates.
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id IN ? AND game_id = ?", userIDs, gameID).Find(&rankings).Error; err != nil {
		return nil, fmt.Errorf("query rankings: %w", err)
	}

	rankingMap := make(map[uint]*db.Ranking)
	for i := range rankings {
		rankingMap[rankings[i].UserID] = &rankings[i]
	}
	return rankingMap, nil
}

func (q *gormMatchRepository) calculateNewElos(
	orderedUserIDs []uint, places []int, rankingMap map[uint]*db.Ranking,
) map[string]float64 {
	players := make([]elo.Player, 0, len(orderedUserIDs))
	for i, userID := range orderedUserIDs {
		rating := elo.DefaultRating
		if r, ok := rankingMap[userID]; ok {
			rating = float64(r.Elo)
		}
		players = append(players, elo.Player{
			ID:     strconv.FormatUint(uint64(userID), 10),
			Rating: rating,
			Place:  placeAt(places, i),
		})
	}
	return elo.Calculate(players)
}

// placeAt is the finishing place of the i-th standing. Callers that have no tie
// information pass nil, which is the strict order the slice already carries.
func placeAt(places []int, i int) int {
	if i < len(places) && places[i] > 0 {
		return places[i]
	}
	return i + 1
}

func (q *gormMatchRepository) recordNewMatch(tx *gorm.DB, gameID uint, ranked bool) (*db.Match, error) {
	match := db.Match{GameID: gameID, Ranked: ranked}
	if err := tx.Create(&match).Error; err != nil {
		return nil, fmt.Errorf("create match: %w", err)
	}
	return &match, nil
}
