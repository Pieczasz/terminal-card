//go:build integration

package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/config"
	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestConnect_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	postgresContainer, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("testdb"),
		tcpostgres.WithUsername("testuser"),
		tcpostgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(time.Second*60),
		),
	)
	testutil.RequireContainer(t, err)
	t.Cleanup(func() {
		_ = postgresContainer.Terminate(ctx)
	})

	host, err := postgresContainer.Host(ctx)
	require.NoError(t, err)
	mappedPort, err := postgresContainer.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)

	cfg := &config.Config{
		DBHost:     host,
		DBPort:     int(mappedPort.Num()),
		DBUser:     "testuser",
		DBPassword: "testpass",
		DBName:     "testdb",
		DBSSLMode:  "disable",
		// The pool cap is DBMaxOpenConnections. MaxConnections is the ssh session cap
		// and setting it here asserted nothing about the pool at all.
		DBMaxOpenConnections: 5,
		MaxConnections:       200,
		Env:                  "production",
	}

	database, err := db.Connect(cfg)
	require.NoError(t, err)
	require.NotNil(t, database)

	sqlDB, err := database.DB()
	require.NoError(t, err)
	require.NotNil(t, sqlDB)

	err = sqlDB.Ping()
	assert.NoError(t, err)

	assert.Equal(t, 5, sqlDB.Stats().MaxOpenConnections,
		"DBMaxOpenConnections has to reach the pool, not just the config struct")
}

// A NULL scans into the Go zero value, so a nullable column whose struct field is a
// plain string/int cannot tell "unset" from "empty" or "zero". Migration 000002
// pinned these down; this test is what makes a future migration that loosens one
// fail CI instead of shipping.
func TestSchemaNullabilityMatchesStructs(t *testing.T) {
	t.Parallel()
	database := testutil.SetupTestDB(t)

	notNull := []struct{ table, column string }{
		{"users", "username"},
		{"public_keys", "fingerprint"},
		{"games", "name"},
		{"match_participants", "placement"},
		{"match_participants", "elo_delta"},
		{"matches", "ranked"},
		{"rankings", "matches_played"},
	}

	for _, want := range notNull {
		var isNullable string
		err := database.Raw(`SELECT is_nullable FROM information_schema.columns
			WHERE table_name = ? AND column_name = ?`, want.table, want.column).
			Scan(&isNullable).Error
		require.NoError(t, err)
		require.NotEmpty(t, isNullable, "%s.%s does not exist", want.table, want.column)
		assert.Equal(t, "NO", isNullable, "%s.%s must be NOT NULL", want.table, want.column)
	}
}

// The leaderboard and history queries are index-shaped; losing one is a silent
// regression to a sequential scan.
func TestSchemaHasHotPathIndexes(t *testing.T) {
	t.Parallel()
	database := testutil.SetupTestDB(t)

	for _, name := range []string{
		"idx_rankings_game_elo",
		"idx_match_participants_user_match",
		"idx_matches_game_id",
		"idx_rankings_game_id",
	} {
		var count int64
		require.NoError(t, database.Raw(
			`SELECT count(*) FROM pg_indexes WHERE indexname = ?`, name).Scan(&count).Error)
		assert.Equal(t, int64(1), count, "index %s is missing", name)
	}
}

func TestConnect_Failure(t *testing.T) {
	cfg := &config.Config{
		DBHost:         "invalid-host",
		DBPort:         5432,
		DBUser:         "user",
		DBPassword:     "pass",
		DBName:         "db",
		DBSSLMode:      "disable",
		MaxConnections: 5,
	}

	database, err := db.Connect(cfg)
	assert.Error(t, err)
	assert.Nil(t, database)
}
