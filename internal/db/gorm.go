package db

import (
	"fmt"
	"time"

	"github.com/Pieczasz/terminal-card/internal/config"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func Connect(cfg *config.Config) (*gorm.DB, error) {
	// Info logs every statement with its parameter values, and logs ship to Loki.
	// Only development opts into that: staging is not production but may still be
	// pointed at real data.
	logMode := logger.Warn
	if cfg.Env == "development" {
		logMode = logger.Info
	}

	database, err := gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logMode),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to the database: %w", err)
	}

	sqlDB, err := database.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(cfg.DBMaxOpenConnections)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return database, nil
}
