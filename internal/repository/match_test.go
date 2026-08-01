//go:build integration

package repository_test

import (
	"context"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/elo"
	"github.com/Pieczasz/terminal-card/internal/repository"
	"github.com/Pieczasz/terminal-card/internal/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchRepository_RecordMatch(t *testing.T) {
	gormDB := testutil.SetupTestDB(t, &db.User{}, &db.Game{}, &db.Match{}, &db.MatchParticipant{}, &db.Ranking{}, &db.PublicKey{})

	// Seed data
	game := &db.Game{Name: "Crazy Eights"}
	require.NoError(t, gormDB.Create(game).Error)

	u1 := &db.User{Username: "player1"}
	u2 := &db.User{Username: "player2"}
	require.NoError(t, gormDB.Create(u1).Error)
	require.NoError(t, gormDB.Create(u2).Error)

	r1 := &db.Ranking{UserID: u1.ID, GameID: game.ID, Elo: uint32(elo.DefaultRating)}
	r2 := &db.Ranking{UserID: u2.ID, GameID: game.ID, Elo: uint32(elo.DefaultRating)}
	require.NoError(t, gormDB.Create(r1).Error)
	require.NoError(t, gormDB.Create(r2).Error)

	repo := repository.NewMatchRepository(gormDB)

	ctx := context.Background()

	orderedUserIDs := []uint{u1.ID, u2.ID}

	deltas, err := repo.UpdateRankings(ctx, game.ID, orderedUserIDs)
	require.NoError(t, err)

	err = repo.RecordMatch(ctx, game.ID, orderedUserIDs, deltas)
	require.NoError(t, err)

	// Verify match was created
	var match db.Match
	err = gormDB.Preload("Participants").First(&match).Error
	require.NoError(t, err)
	assert.Equal(t, game.ID, match.GameID)
	assert.Len(t, match.Participants, 2)

	// Verify Rankings were updated
	var updatedR1 db.Ranking
	gormDB.Where("user_id = ? AND game_id = ?", r1.UserID, r1.GameID).First(&updatedR1)
	assert.Greater(t, updatedR1.Elo, uint32(elo.DefaultRating))

	var updatedR2 db.Ranking
	gormDB.Where("user_id = ? AND game_id = ?", r2.UserID, r2.GameID).First(&updatedR2)
	assert.Less(t, updatedR2.Elo, uint32(elo.DefaultRating))
}

func TestMatchRepository_FinalizeRankedMatch(t *testing.T) {
	gormDB := testutil.SetupTestDB(t, &db.User{}, &db.Game{}, &db.Match{}, &db.MatchParticipant{}, &db.Ranking{}, &db.PublicKey{})

	u1 := &db.User{Username: "final1"}
	u2 := &db.User{Username: "final2"}
	require.NoError(t, gormDB.Create(u1).Error)
	require.NoError(t, gormDB.Create(u2).Error)

	repo := repository.NewMatchRepository(gormDB)
	ctx := context.Background()

	err := repo.FinalizeRankedMatch(ctx, "Crazy Eights", []uint{u1.ID, u2.ID})
	require.NoError(t, err)

	var game db.Game
	require.NoError(t, gormDB.Where("name = ?", "Crazy Eights").First(&game).Error)

	var matchCount int64
	require.NoError(t, gormDB.Model(&db.Match{}).Where("game_id = ?", game.ID).Count(&matchCount).Error)
	assert.Equal(t, int64(1), matchCount)

	var r1 db.Ranking
	require.NoError(t, gormDB.Where("user_id = ? AND game_id = ?", u1.ID, game.ID).First(&r1).Error)
	assert.Greater(t, r1.Elo, uint32(elo.DefaultRating))
}

func TestMatchRepository_GetOrCreateGame(t *testing.T) {
	gormDB := testutil.SetupTestDB(t, &db.Game{})
	repo := repository.NewMatchRepository(gormDB)

	ctx := context.Background()
	game1, err := repo.GetOrCreateGame(ctx, "Poker")
	require.NoError(t, err)
	assert.Equal(t, "Poker", game1.Name)

	game2, err := repo.GetOrCreateGame(ctx, "Poker")
	require.NoError(t, err)
	assert.Equal(t, game1.ID, game2.ID)
}

func TestMatchRepository_RecordMatch_TransactionError(t *testing.T) {
	gormDB := testutil.SetupTestDB(t, &db.User{}, &db.Game{}, &db.Match{}, &db.MatchParticipant{}, &db.Ranking{}, &db.PublicKey{})
	repo := repository.NewMatchRepository(gormDB)

	ctx := context.Background()

	// Invalid users
	orderedUserIDs := []uint{9999, 9998}

	_, err := repo.UpdateRankings(ctx, 1, []uint{})
	assert.NoError(t, err) // Should return nil for empty user IDs

	err = repo.RecordMatch(ctx, 1, []uint{}, map[uint]int{})
	assert.NoError(t, err) // Should return nil for empty user IDs

	// UpdateRankings with non-existent users should not fail inherently in fetching, but calculateNewElos will handle them as DefaultRating.
	// However, the transaction should fail when creating Ranking if the foreign key (user_id) constraint is violated.
	_, err = repo.UpdateRankings(ctx, 1, orderedUserIDs)
	assert.Error(t, err) // Foreign key constraint violation on Ranking

	err = repo.RecordMatch(ctx, 1, orderedUserIDs, map[uint]int{9999: 10, 9998: -10})
	assert.Error(t, err) // Foreign key constraint violation on Match / MatchParticipant
}
