//go:build integration

package repository_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/elo"
	"github.com/Pieczasz/terminal-card/internal/repository"
	"github.com/Pieczasz/terminal-card/internal/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMatchRepositoryRecordCasualMatch(t *testing.T) {
	gormDB := testutil.SetupTestDB(t)

	u1 := &db.User{Username: "player1"}
	u2 := &db.User{Username: "player2"}
	require.NoError(t, gormDB.Create(u1).Error)
	require.NoError(t, gormDB.Create(u2).Error)

	repo := repository.NewMatchRepository(gormDB)
	ctx := context.Background()

	require.NoError(t, repo.RecordCasualMatch(ctx, "Crazy Eights", []uint{u1.ID, u2.ID}))

	var match db.Match
	require.NoError(t, gormDB.Preload("Participants").First(&match).Error)
	assert.False(t, match.Ranked, "a casual match is recorded without changing Elo")
	assert.Len(t, match.Participants, 2)

	var game db.Game
	require.NoError(t, gormDB.Where("name = ?", "Crazy Eights").First(&game).Error)
	assert.Equal(t, game.ID, match.GameID, "the game row is created on first sight")

	// A casual result must not create rankings.
	var rankings int64
	require.NoError(t, gormDB.Model(&db.Ranking{}).Count(&rankings).Error)
	assert.Zero(t, rankings)
}

// getOrCreateGame is an internal transaction helper, so its idempotence is only
// observable through a second RecordCasualMatch reusing the same games row.
func TestMatchRepositoryReusesTheGameRow(t *testing.T) {
	gormDB := testutil.SetupTestDB(t)

	u := &db.User{Username: "solo"}
	require.NoError(t, gormDB.Create(u).Error)

	repo := repository.NewMatchRepository(gormDB)
	ctx := context.Background()

	require.NoError(t, repo.RecordCasualMatch(ctx, "Poker", []uint{u.ID}))
	require.NoError(t, repo.RecordCasualMatch(ctx, "Poker", []uint{u.ID}))

	var games []db.Game
	require.NoError(t, gormDB.Where("name = ?", "Poker").Find(&games).Error)
	require.Len(t, games, 1, "the second match must reuse the first game row")

	var matches int64
	require.NoError(t, gormDB.Model(&db.Match{}).Where("game_id = ?", games[0].ID).Count(&matches).Error)
	assert.Equal(t, int64(2), matches)
}

func TestMatchRepositoryFinalizeRankedMatch(t *testing.T) {
	gormDB := testutil.SetupTestDB(t)

	u1 := &db.User{Username: "final1"}
	u2 := &db.User{Username: "final2"}
	require.NoError(t, gormDB.Create(u1).Error)
	require.NoError(t, gormDB.Create(u2).Error)

	repo := repository.NewMatchRepository(gormDB)
	ctx := context.Background()

	require.NoError(t, repo.FinalizeRankedMatch(ctx, "Crazy Eights", []uint{u1.ID, u2.ID}, nil))

	var game db.Game
	require.NoError(t, gormDB.Where("name = ?", "Crazy Eights").First(&game).Error)

	var matchCount int64
	require.NoError(t, gormDB.Model(&db.Match{}).Where("game_id = ?", game.ID).Count(&matchCount).Error)
	assert.Equal(t, int64(1), matchCount)

	var r1 db.Ranking
	require.NoError(t, gormDB.Where("user_id = ? AND game_id = ?", u1.ID, game.ID).First(&r1).Error)
	assert.Greater(t, r1.Elo, uint32(elo.DefaultRating))
}

func TestMatchRepositoryRejectsUnknownUsers(t *testing.T) {
	gormDB := testutil.SetupTestDB(t)
	repo := repository.NewMatchRepository(gormDB)
	ctx := context.Background()

	assert.NoError(t, repo.RecordCasualMatch(ctx, "Poker", nil), "no players is a no-op")
	assert.Error(t, repo.RecordCasualMatch(ctx, "Poker", []uint{9999, 9998}),
		"the participants foreign key must reject users that do not exist")
}

// A repeated seat would move that player's rating twice before colliding on the
// match_participants primary key and rolling the whole match back.
func TestMatchRepositoryRejectsDuplicateUserIDs(t *testing.T) {
	gormDB := testutil.SetupTestDB(t)

	u := &db.User{Username: "twice"}
	require.NoError(t, gormDB.Create(u).Error)

	repo := repository.NewMatchRepository(gormDB)
	ctx := context.Background()

	require.ErrorContains(t, repo.FinalizeRankedMatch(ctx, "Poker", []uint{u.ID, u.ID}, nil),
		"duplicate user id")
	require.ErrorContains(t, repo.RecordCasualMatch(ctx, "Poker", []uint{u.ID, u.ID}),
		"duplicate user id")

	var matches int64
	require.NoError(t, gormDB.Model(&db.Match{}).Count(&matches).Error)
	assert.Zero(t, matches, "the duplicate is rejected before anything is written")
}

// Reproduction: a brand-new (user_id, game_id) has no row for the FOR UPDATE select
// to lock, so two concurrent finalizes both inserted the ranking, one lost to a
// unique violation and its entire match - Elo and history - was rolled back.
func TestMatchRepositoryConcurrentFinalizeFirstEverMatch(t *testing.T) {
	gormDB := testutil.SetupTestDB(t)

	u1 := &db.User{Username: "fresh1"}
	u2 := &db.User{Username: "fresh2"}
	require.NoError(t, gormDB.Create(u1).Error)
	require.NoError(t, gormDB.Create(u2).Error)

	repo := repository.NewMatchRepository(gormDB)

	const workers = 4
	errs := make(chan error, workers)
	start := make(chan struct{})
	for range workers {
		go func() {
			<-start
			errs <- repo.FinalizeRankedMatch(context.Background(), "Poker", []uint{u1.ID, u2.ID}, nil)
		}()
	}
	close(start)
	for range workers {
		require.NoError(t, <-errs, "a first-ever ranking must not lose the match to a write conflict")
	}

	var matches int64
	require.NoError(t, gormDB.Model(&db.Match{}).Count(&matches).Error)
	assert.Equal(t, int64(workers), matches, "every finalize has to leave history behind")

	var rankings int64
	require.NoError(t, gormDB.Model(&db.Ranking{}).Count(&rankings).Error)
	assert.Equal(t, int64(2), rankings, "one ranking row per player, not one per match")
}

// Proves the FOR UPDATE lock actually serializes: concurrent finalizes on an
// existing ranking must land on the same Elo as running them one after another.
func TestMatchRepositoryConcurrentFinalizeMatchesSequential(t *testing.T) {
	// Two rounds on top of the seed keeps the pair under maxSamePairingPerDay in both
	// orders: run sequentially the fourth would be damped, run concurrently the
	// uncommitted siblings are invisible to each other and none would be.
	const rounds = 2

	elos := func(t *testing.T, concurrent bool) (uint32, uint32) {
		t.Helper()
		gormDB := testutil.SetupTestDB(t)

		u1 := &db.User{Username: "seq1"}
		u2 := &db.User{Username: "seq2"}
		require.NoError(t, gormDB.Create(u1).Error)
		require.NoError(t, gormDB.Create(u2).Error)

		repo := repository.NewMatchRepository(gormDB)
		ctx := context.Background()

		// Seed the rankings so this exercises the locking path, not the seeding one.
		require.NoError(t, repo.FinalizeRankedMatch(ctx, "Poker", []uint{u1.ID, u2.ID}, nil))

		run := func() error { return repo.FinalizeRankedMatch(ctx, "Poker", []uint{u1.ID, u2.ID}, nil) }
		if concurrent {
			errs := make(chan error, rounds)
			start := make(chan struct{})
			for range rounds {
				go func() {
					<-start
					errs <- run()
				}()
			}
			close(start)
			for range rounds {
				require.NoError(t, <-errs)
			}
		} else {
			for range rounds {
				require.NoError(t, run())
			}
		}

		var got []db.Ranking
		require.NoError(t, gormDB.Where("user_id IN ?", []uint{u1.ID, u2.ID}).
			Order("user_id").Find(&got).Error)
		require.Len(t, got, 2)
		return got[0].Elo, got[1].Elo
	}

	wantWinner, wantLoser := elos(t, false)
	gotWinner, gotLoser := elos(t, true)

	assert.Equal(t, wantWinner, gotWinner, "a lost update would leave the winner short")
	assert.Equal(t, wantLoser, gotLoser)
}

// Reproduction: a ranked match must still read back as ranked through the history
// the profile screen renders from. The write and the read are in different
// repositories, so each being correct on its own does not prove the pair is.
func TestRankedMatchReadsBackAsRankedInHistory(t *testing.T) {
	gormDB := testutil.SetupTestDB(t)

	u1 := &db.User{Username: "winner"}
	u2 := &db.User{Username: "loser"}
	require.NoError(t, gormDB.Create(u1).Error)
	require.NoError(t, gormDB.Create(u2).Error)

	matches := repository.NewMatchRepository(gormDB)
	users := repository.NewUserRepository(gormDB)
	ctx := context.Background()

	require.NoError(t, matches.FinalizeRankedMatch(ctx, "Poker", []uint{u1.ID, u2.ID}, nil))

	var stored db.Match
	require.NoError(t, gormDB.First(&stored).Error)
	assert.True(t, stored.Ranked, "the row itself has to say ranked")

	history, err := users.UserMatchHistory(ctx, u1.ID, 10)
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.True(t, history[0].Match.Ranked,
		"the profile reads Match.Ranked from here, so it has to survive the preload")
	assert.Equal(t, "Poker", history[0].Match.Game.Name)
	assert.NotZero(t, history[0].EloDelta, "a ranked match moved the rating")
}

// seat is a starting ranking row: matches_played below the provisional threshold is
// what makes the seat provisional, and distinct Elos keep every seat's natural delta
// nonzero (equal ratings cancel out to zero for the middle seats of a table, which
// would make "the delta was zeroed" unfalsifiable).
type seat struct {
	elo    uint32
	played uint64
}

// antiFarmTable creates the game and one user per seat, pre-seeding the ranking rows
// so a track record does not cost five real matches to build.
func antiFarmTable(t *testing.T, gormDB *gorm.DB, gameName string, seats ...seat) (uint, []uint) {
	t.Helper()

	game := db.Game{Name: gameName}
	require.NoError(t, gormDB.Create(&game).Error)

	userIDs := make([]uint, 0, len(seats))
	for i, s := range seats {
		u := db.User{Username: fmt.Sprintf("seat%d", i)}
		require.NoError(t, gormDB.Create(&u).Error)
		require.NoError(t, gormDB.Create(&db.Ranking{
			UserID: u.ID, GameID: game.ID, Elo: s.elo, MatchesPlayed: s.played,
		}).Error)
		userIDs = append(userIDs, u.ID)
	}
	return game.ID, userIDs
}

func rankingOf(t *testing.T, gormDB *gorm.DB, userID, gameID uint) db.Ranking {
	t.Helper()
	var r db.Ranking
	require.NoError(t, gormDB.Where("user_id = ? AND game_id = ?", userID, gameID).First(&r).Error)
	return r
}

// lastMatchDeltas is the elo_delta history of the most recent match, which is where a
// damped or unpaid result has to show up as a zero rather than as nothing at all.
func lastMatchDeltas(t *testing.T, gormDB *gorm.DB) map[uint]int {
	t.Helper()
	var match db.Match
	require.NoError(t, gormDB.Preload("Participants").Order("id DESC").First(&match).Error)

	deltas := make(map[uint]int, len(match.Participants))
	for _, p := range match.Participants {
		deltas[p.UserID] = p.EloDelta
	}
	return deltas
}

// Minting an account is free, so beating a fresh one must pay nothing - while the
// fresh account's own rating still converges.
func TestFinalizeRankedMatchProvisionalOpponentPaysNothing(t *testing.T) {
	tests := []struct {
		name  string
		seats []seat
	}{
		{"heads up against a fresh account", []seat{{1500, 10}, {1500, 0}}},
		{"one fresh account at a table of four", []seat{{1500, 10}, {1450, 0}, {1300, 12}, {1100, 7}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gormDB := testutil.SetupTestDB(t)
			gameID, userIDs := antiFarmTable(t, gormDB, "Poker", tt.seats...)

			repo := repository.NewMatchRepository(gormDB)
			require.NoError(t, repo.FinalizeRankedMatch(context.Background(), "Poker", userIDs, nil))

			deltas := lastMatchDeltas(t, gormDB)
			require.Len(t, deltas, len(tt.seats), "history is written either way")

			for i, s := range tt.seats {
				r := rankingOf(t, gormDB, userIDs[i], gameID)
				assert.Equal(t, s.played+1, r.MatchesPlayed, "seat %d has played another match", i)

				if s.played < 5 {
					assert.NotEqual(t, s.elo, r.Elo, "the provisional seat %d still converges", i)
					assert.NotZero(t, deltas[userIDs[i]], "and its own history records the move")
					continue
				}
				assert.Equal(t, s.elo, r.Elo, "established seat %d must not be paid by a provisional", i)
				assert.Zero(t, deltas[userIDs[i]], "and its history delta is zeroed too")
			}
		})
	}
}

// Two accounts trading wins all day is farming or a private rivalry; either way the
// fourth match of the day between the same set stops paying.
func TestFinalizeRankedMatchDampsRepeatedPairing(t *testing.T) {
	gormDB := testutil.SetupTestDB(t)
	gameID, userIDs := antiFarmTable(t, gormDB, "Poker", seat{1500, 10}, seat{1500, 10})

	repo := repository.NewMatchRepository(gormDB)
	ctx := context.Background()

	for range 3 {
		require.NoError(t, repo.FinalizeRankedMatch(ctx, "Poker", userIDs, nil))
	}
	before := []db.Ranking{
		rankingOf(t, gormDB, userIDs[0], gameID),
		rankingOf(t, gormDB, userIDs[1], gameID),
	}
	require.NotEqual(t, uint32(1500), before[0].Elo, "the first three still move rating")

	require.NoError(t, repo.FinalizeRankedMatch(ctx, "Poker", userIDs, nil))

	deltas := lastMatchDeltas(t, gormDB)
	require.Len(t, deltas, 2, "the damped match is still recorded")
	for i, userID := range userIDs {
		after := rankingOf(t, gormDB, userID, gameID)
		assert.Equal(t, before[i].Elo, after.Elo, "the fourth pairing of the day moves nothing")
		assert.Zero(t, deltas[userID], "and records a zero delta")
		assert.Equal(t, before[i].MatchesPlayed+1, after.MatchesPlayed,
			"a damped match is still a match played, or a farmer never graduates")
	}

	var matches int64
	require.NoError(t, gormDB.Model(&db.Match{}).Count(&matches).Error)
	assert.Equal(t, int64(4), matches)
}

// matches_played is what a provisional account graduates on, so a finalize that
// forgot to increment it would leave every account provisional forever.
func TestFinalizeRankedMatchCountsMatchesPlayed(t *testing.T) {
	gormDB := testutil.SetupTestDB(t)

	u1 := &db.User{Username: "counted1"}
	u2 := &db.User{Username: "counted2"}
	require.NoError(t, gormDB.Create(u1).Error)
	require.NoError(t, gormDB.Create(u2).Error)

	repo := repository.NewMatchRepository(gormDB)
	ctx := context.Background()

	require.NoError(t, repo.FinalizeRankedMatch(ctx, "Poker", []uint{u1.ID, u2.ID}, nil))
	require.NoError(t, repo.FinalizeRankedMatch(ctx, "Poker", []uint{u1.ID, u2.ID}, nil))

	var game db.Game
	require.NoError(t, gormDB.Where("name = ?", "Poker").First(&game).Error)

	for _, u := range []*db.User{u1, u2} {
		r := rankingOf(t, gormDB, u.ID, game.ID)
		assert.Equal(t, uint64(2), r.MatchesPlayed, "a seeded row starts at zero and counts up")
		// Both seats are provisional, so both ratings still move.
		assert.NotEqual(t, uint32(elo.DefaultRating), r.Elo)
	}
}

// Soft-deleted rankings used to make seed's ON CONFLICT DO NOTHING leave fetch
// empty, aborting the whole table's Elo write.
func TestFinalizeRankedMatchRevivesSoftDeletedRanking(t *testing.T) {
	gormDB := testutil.SetupTestDB(t)
	gameID, userIDs := antiFarmTable(t, gormDB, "Poker", seat{1600, 3}, seat{1400, 3})

	require.NoError(t, gormDB.Where("user_id = ? AND game_id = ?", userIDs[0], gameID).
		Delete(&db.Ranking{}).Error)

	repo := repository.NewMatchRepository(gormDB)
	require.NoError(t, repo.FinalizeRankedMatch(context.Background(), "Poker", userIDs, nil))

	r := rankingOf(t, gormDB, userIDs[0], gameID)
	assert.False(t, r.DeletedAt.Valid, "the soft-deleted row is revived")
	assert.Equal(t, uint64(4), r.MatchesPlayed)
}
