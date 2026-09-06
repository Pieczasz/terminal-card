package repository

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"log/slog"
	"slices"
	"strconv"
	"time"

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

// endSpan closes a span, marking it failed on error: recording the error without the
// status leaves the one failed span reading as successful.
func endSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

// A duplicate seat would move that player's rating twice, then collide on the
// match_participants primary key and roll the whole match back.
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

// getOrCreateGame takes the handle rather than q.db so a caller inside a transaction
// reuses its connection: going back to the pool holds one while waiting for a second, so
// DBMaxOpenConnections concurrent finalizes deadlock until they time out - and the game
// row would outlive a rollback.
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
		return fmt.Errorf("record casual match: %w", err)
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

		deltas, err := q.updateRankingsTx(ctx, tx, game.ID, orderedUserIDs, places)
		if err != nil {
			return err
		}
		return q.recordMatchTx(tx, game.ID, orderedUserIDs, places, deltas, true)
	}); err != nil {
		return fmt.Errorf("finalize ranked match: %w", err)
	}
	return nil
}

// seedRankingRows gives fetchRankings something to lock. A first-time (user_id, game_id)
// has no row, so FOR UPDATE locks nothing and two concurrent finalizes both insert: one
// hits 23505, rolls back and loses the match. Sorted by user_id because a conflicting
// insert still waits on the inserting transaction, though the ordering that matters is
// fetchRankings' - ON CONFLICT DO NOTHING takes no lock on an existing row.
//
// Soft-deleted rankings are revived first: DO NOTHING leaves them invisible to the
// default scope, and a missing row used to abort the whole table's finalize.
func seedRankingRows(tx *gorm.DB, gameID uint, userIDs []uint) error {
	if err := tx.Unscoped().Model(&db.Ranking{}).
		Where("user_id IN ? AND game_id = ? AND deleted_at IS NOT NULL", userIDs, gameID).
		Update("deleted_at", nil).Error; err != nil {
		return fmt.Errorf("revive rankings: %w", err)
	}
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

const (
	// provisionalMatches is how many ranked results a ranking row needs before
	// beating its owner pays anything. Identity here is a free SSH keypair, so a
	// fresh 1500-rated account is free to mint and farming it must not be
	// profitable until it has a track record of its own.
	provisionalMatches = 5

	// maxSamePairingPerDay is how many ranked matches between the exact same set of
	// players still move rating inside a 24h window. The same pair trading wins is
	// either farming or a private rivalry; either way the ladder stops paying after
	// three a day.
	maxSamePairingPerDay = 3
)

func (q *gormMatchRepository) updateRankingsTx(
	ctx context.Context, tx *gorm.DB, gameID uint, orderedUserIDs []uint, places []int,
) (map[uint]int, error) {
	// Serialize same-pairing finalizes across every game: ranking row locks are
	// per (user, game), so A–B farming Poker and Hearts concurrently would both
	// see an undamped count without this.
	if err := lockPairing(tx, orderedUserIDs); err != nil {
		return nil, err
	}

	if err := seedRankingRows(tx, gameID, orderedUserIDs); err != nil {
		return nil, err
	}

	rankingMap, err := q.fetchRankings(tx, gameID, orderedUserIDs)
	if err != nil {
		return nil, fmt.Errorf("fetch rankings: %w", err)
	}

	pairings, err := samePairingCountLast24h(tx, orderedUserIDs)
	if err != nil {
		return nil, err
	}
	damped := pairings >= maxSamePairingPerDay
	if damped {
		slog.WarnContext(ctx, "ranked match damped: same players again inside 24h",
			"user_ids", orderedUserIDs, "game_id", gameID, "recent_pairings", pairings)
	}

	newRatings := q.calculateNewElos(orderedUserIDs, places, rankingMap)
	provisional := anyProvisional(orderedUserIDs, rankingMap)

	deltas := make(map[uint]int, len(orderedUserIDs))
	for _, userID := range orderedUserIDs {
		// Every seat was just seeded, so a miss is a soft-deleted row, not a new player.
		r, ok := rankingMap[userID]
		if !ok {
			return nil, fmt.Errorf("no ranking row for user %d in game %d", userID, gameID)
		}
		// A key mismatch used to fall through to the zero value and store the elo floor.
		newRating, ok := newRatings[strconv.FormatUint(uint64(userID), 10)]
		if !ok {
			return nil, fmt.Errorf("no elo result for user %d", userID)
		}

		// The increment rides the row this transaction already holds FOR UPDATE, and
		// happens whether or not the rating moved: a damped or unpaid match is still a
		// match played, and it is what lets a provisional account graduate.
		update := map[string]any{"matches_played": gorm.Expr("matches_played + 1")}

		// A provisional player's own rating still converges; only the established
		// players around them go unpaid. This deliberately breaks conservation - the
		// zero-sum property of Elo is worth less than an unfarmable ladder.
		if !damped && (!provisional || r.MatchesPlayed < provisionalMatches) {
			stored := elo.ToUint32(newRating)
			update["elo"] = stored
			deltas[userID] = int(stored) - int(r.Elo)
		}
		if err := tx.Model(r).Updates(update).Error; err != nil {
			return nil, fmt.Errorf("update ranking: %w", err)
		}
	}
	return deltas, nil
}

func anyProvisional(orderedUserIDs []uint, rankingMap map[uint]*db.Ranking) bool {
	for _, userID := range orderedUserIDs {
		if r, ok := rankingMap[userID]; ok && r.MatchesPlayed < provisionalMatches {
			return true
		}
	}
	return false
}

// samePairingCountLast24h counts recent ranked matches whose participant set is
// exactly userIDs, across every game - a pair farming each other does not become
// legitimate by switching table. One participant's recent matches bound the scan and
// the sets are compared in Go, which is cheap at these volumes. Callers must hold
// lockPairing so concurrent finalizes of the same set cannot both underrun the cap.
func samePairingCountLast24h(tx *gorm.DB, userIDs []uint) (int, error) {
	var matchIDs []uint
	if err := tx.Model(&db.MatchParticipant{}).
		Joins("JOIN matches ON matches.id = match_participants.match_id").
		Where(`match_participants.user_id = ? AND matches.ranked
			AND matches.deleted_at IS NULL AND matches.created_at > ?`,
			userIDs[0], time.Now().Add(-24*time.Hour)).
		Pluck("match_participants.match_id", &matchIDs).Error; err != nil {
		return 0, fmt.Errorf("query recent pairings: %w", err)
	}
	if len(matchIDs) == 0 {
		return 0, nil
	}

	var rows []db.MatchParticipant
	if err := tx.Where("match_id IN ?", matchIDs).Find(&rows).Error; err != nil {
		return 0, fmt.Errorf("query pairing participants: %w", err)
	}

	seats := make(map[uint][]uint, len(matchIDs))
	for _, row := range rows {
		seats[row.MatchID] = append(seats[row.MatchID], row.UserID)
	}

	want := slices.Sorted(slices.Values(userIDs))
	count := 0
	for _, got := range seats {
		if slices.Equal(want, slices.Sorted(slices.Values(got))) {
			count++
		}
	}
	return count, nil
}

func lockPairing(tx *gorm.DB, userIDs []uint) error {
	if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", pairingAdvisoryKey(userIDs)).Error; err != nil {
		return fmt.Errorf("lock pairing: %w", err)
	}
	return nil
}

func pairingAdvisoryKey(userIDs []uint) int64 {
	h := fnv.New64a()
	for _, id := range slices.Sorted(slices.Values(userIDs)) {
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], uint64(id))
		_, _ = h.Write(buf[:])
	}
	// Advisory keys are opaque coordination tokens, not secrets.
	return int64(h.Sum64()) //nolint:gosec // G115: lock key space is intentionally 64-bit wrap
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
	// One multi-row insert rather than a round trip per seat.
	if err := tx.Create(&participants).Error; err != nil {
		return fmt.Errorf("create match participants: %w", err)
	}
	return nil
}

func (q *gormMatchRepository) fetchRankings(tx *gorm.DB, gameID uint, userIDs []uint) (map[uint]*db.Ranking, error) {
	var rankings []db.Ranking
	// FOR UPDATE: serialize concurrent finalize transactions to avoid lost Elo updates.
	//
	// ORDER BY user_id is what makes that safe rather than deadlock-prone: this is
	// where the row locks are actually taken (the seed's ON CONFLICT DO NOTHING locks
	// nothing when the row already exists, which is the common case), so without a
	// fixed order two finalizes over overlapping seats can lock in opposite orders.
	// Postgres then aborts one, and that match is lost from history and Elo.
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id IN ? AND game_id = ?", userIDs, gameID).
		Order("user_id").Find(&rankings).Error; err != nil {
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
