//go:build integration

package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/config"
	"github.com/Pieczasz/terminal-card/internal/db"

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
	if err != nil {
		t.Skipf("skipping test because Docker provider is not available: %v", err)
	}
	t.Cleanup(func() {
		_ = postgresContainer.Terminate(ctx)
	})

	host, err := postgresContainer.Host(ctx)
	require.NoError(t, err)
	mappedPort, err := postgresContainer.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)

	cfg := &config.Config{
		DBHost:         host,
		DBPort:         int(mappedPort.Num()),
		DBUser:         "testuser",
		DBPassword:     "testpass",
		DBName:         "testdb",
		DBSSLMode:      "disable",
		MaxConnections: 5,
		Env:            "production",
	}

	database, err := db.Connect(cfg)
	require.NoError(t, err)
	require.NotNil(t, database)

	sqlDB, err := database.DB()
	require.NoError(t, err)
	require.NotNil(t, sqlDB)

	err = sqlDB.Ping()
	assert.NoError(t, err)
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
