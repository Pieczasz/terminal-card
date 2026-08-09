//go:build integration

package testutil

import (
	"context"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/db"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// RequireContainer skips when the container failed to start because Docker is
// absent, and fails on anything else - a broken image or a startup timeout is a
// real failure and must not disappear into a skip.
func RequireContainer(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	errStr := strings.ToLower(err.Error())

	// A reply from the daemon means Docker is there and something else went wrong: a
	// bad image tag, a registry outage, a startup timeout. Those have to fail. The
	// bare substrings "docker" and "daemon" match those replies too - "error response
	// from daemon: manifest for postgres:16-alpine not found" contains both - which
	// turned every real failure into a skip and a green pipeline. Since these tests
	// are the only thing checking the SQL migrations against the GORM models, that is
	// exactly the failure that must not be silent.
	if !strings.Contains(errStr, "response from daemon") {
		for _, marker := range []string{
			"cannot connect to the docker daemon",
			"is the docker daemon running",
			"docker daemon is not running",
			"rootless docker not found",
			"failed to find a viable docker provider",
		} {
			if strings.Contains(errStr, marker) {
				t.Skipf("skipping test because Docker is not available or not running: %v", err)
			}
		}
	}
	t.Fatalf("failed to start postgres container: %v", err)
}

// SetupTestDB brings up a Postgres container and applies the production
// migrations, so tests see the constraints and indexes the deployed schema has.
func SetupTestDB(t *testing.T) *gorm.DB {
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
	RequireContainer(t, err)

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

	applyMigrations(t, gormDB)

	return gormDB
}

func applyMigrations(t *testing.T, gormDB *gorm.DB) {
	t.Helper()

	steps, err := fs.Glob(db.Migrations, "migrations/*.up.sql")
	if err != nil {
		t.Fatalf("failed to list migrations: %v", err)
	}
	slices.Sort(steps)

	for _, step := range steps {
		sql, err := db.Migrations.ReadFile(step)
		if err != nil {
			t.Fatalf("failed to read migration %s: %v", step, err)
		}
		if err := gormDB.Exec(string(sql)).Error; err != nil {
			t.Fatalf("failed to apply migration %s: %v", step, err)
		}
	}
}
