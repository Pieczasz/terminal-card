package db

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/Pieczasz/terminal-card/internal/config"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func Connect(cfg *config.Config) (*gorm.DB, error) {
	// Info logs every statement. Only development opts into that: staging is not
	// production but may still be pointed at real data.
	logMode := logger.Warn
	if cfg.Env == "development" {
		logMode = logger.Info
	}

	database, err := gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{
		// GORM's default logger writes to stdout, which nothing collects; routing it
		// through slog puts it in the same stream as everything else.
		Logger: logger.NewSlogLogger(slog.Default(), logger.Config{
			LogLevel:      logMode,
			SlowThreshold: 200 * time.Millisecond,
			// Registration probes a fingerprint that is usually absent, so a miss is
			// the normal case rather than an error.
			IgnoreRecordNotFoundError: true,
			// Parameter values include key fingerprints. Nothing needs them in a log
			// line, and Loki keeps them far longer than the query.
			ParameterizedQueries: true,
		}),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to the database: %w", err)
	}

	sqlDB, err := database.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	sqlDB.SetMaxOpenConns(cfg.DBMaxOpenConnections)
	// database/sql silently clamps idle down to the open cap, which hid a
	// misconfigured pool; derive it instead so the two cannot disagree.
	sqlDB.SetMaxIdleConns(min(10, cfg.DBMaxOpenConnections))
	sqlDB.SetConnMaxLifetime(time.Hour)
	// Postgres runs a backend process per connection, so a quiet server should not
	// pin idle ones for a full lifetime.
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)

	return database, nil
}
