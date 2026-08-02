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

func TestMatchRepositoryRecordMatch(t *testing.T) {
	gormDB := testutil.SetupTestDB(t, &db.User{}, &db.Game{}, &db.Match{}, &db.MatchParticipant{}, &db.PublicKey{})

	// Seed data
	game := &db.Game{Name: "Crazy Eights"}
	require.NoError(t, gormDB.Create(game).Error)

	u1 := &db.User{Username: "player1"}
	u2 := &db.User{Username: "player2"}
	require.NoError(t, gormDB.Create(u1).Error)
	require.NoError(t, gormDB.Create(u2).Error)

	repo := repository.NewMatchRepository(gormDB)

	ctx := context.Background()

	orderedUserIDs := []uint{u1.ID, u2.ID}

	err := repo.RecordMatch(ctx, game.ID, orderedUserIDs, nil, false)
	require.NoError(t, err)

	var match db.Match
	err = gormDB.Preload("Participants").First(&match).Error
	require.NoError(t, err)
	assert.Equal(t, game.ID, match.GameID)
	assert.False(t, match.Ranked, "a casual match is recorded without changing Elo")
	assert.Len(t, match.Participants, 2)
}

func TestMatchRepositoryFinalizeRankedMatch(t *testing.T) {
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

func TestMatchRepositoryGetOrCreateGame(t *testing.T) {
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

func TestMatchRepositoryRecordMatchTransactionError(t *testing.T) {
	gormDB := testutil.SetupTestDB(t, &db.User{}, &db.Game{}, &db.Match{}, &db.MatchParticipant{}, &db.PublicKey{})
	repo := repository.NewMatchRepository(gormDB)

	ctx := context.Background()

	orderedUserIDs := []uint{9999, 9998}

	err := repo.RecordMatch(ctx, 1, []uint{}, nil, false)
	assert.NoError(t, err)

	err = repo.RecordMatch(ctx, 1, orderedUserIDs, nil, false)
	assert.Error(t, err)
}
