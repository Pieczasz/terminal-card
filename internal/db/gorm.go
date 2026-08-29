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
	// Only development logs every statement: staging may still point at real data.
	logMode := logger.Warn
	if cfg.Env == "development" {
		logMode = logger.Info
	}

	database, err := gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{
		// GORM's default logger writes to stdout, which nothing collects.
		Logger: logger.NewSlogLogger(slog.Default(), logger.Config{
			LogLevel:      logMode,
			SlowThreshold: 200 * time.Millisecond,
			// Registration probes a fingerprint that is usually absent.
			IgnoreRecordNotFoundError: true,
			// Parameter values include key fingerprints, and Loki keeps them far longer
			// than the query.
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
	// database/sql silently clamps idle to the open cap, which hid a misconfigured pool.
	sqlDB.SetMaxIdleConns(min(10, cfg.DBMaxOpenConnections))
	sqlDB.SetConnMaxLifetime(time.Hour)
	// Postgres runs a backend process per connection, so a quiet server should not pin
	// idle ones for a full lifetime.
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)

	return database, nil
}
