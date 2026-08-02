//go:build integration

package testutil

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func SetupTestDB(t *testing.T, models ...any) *gorm.DB {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping DB integration test in short mode")
	}

	_, err := testcontainers.ProviderDocker.GetProvider()
	if err != nil {
		t.Skipf("skipping test because Docker provider is not available: %v", err)
	}

	ctx := context.Background()

	postgresContainer, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("test"),
		tcpostgres.WithUsername("user"),
		tcpostgres.WithPassword("password"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(time.Second*60),
		),
	)
	if err != nil {
		errStr := strings.ToLower(err.Error())
		for _, marker := range []string{"docker", "daemon", "provider"} {
			if strings.Contains(errStr, marker) {
				t.Skipf("skipping test because Docker is not available or not running: %v", err)
			}
		}
		t.Fatalf("failed to start postgres container: %v", err)
	}

	t.Cleanup(func() {
		if err := postgresContainer.Terminate(ctx); err != nil {
			t.Errorf("failed to terminate container: %v", err)
		}
	})

	connStr, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get postgres connection string: %v", err)
	}

	gormDB, err := gorm.Open(gormpostgres.Open(connStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}

	// Close the pool before the container goes away. Without this each test leaks
	// sql.DB's connection-opener and cleaner goroutines for the rest of the run.
	t.Cleanup(func() {
		sqlDB, err := gormDB.DB()
		if err != nil {
			t.Errorf("failed to reach the sql.DB for teardown: %v", err)
			return
		}
		if err := sqlDB.Close(); err != nil {
			t.Errorf("failed to close the database pool: %v", err)
		}
	})

	if len(models) > 0 {
		err = gormDB.AutoMigrate(models...)
		if err != nil {
			t.Fatalf("failed to run migrations: %v", err)
		}
	}

	return gormDB
}
