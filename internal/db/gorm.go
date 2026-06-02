package db

import (
	"client/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"log/slog"
)

func Connect(cfg *config.Config) (*gorm.DB, error) {
	logMode := logger.Info
	if cfg.Env == "production" || cfg.Env == "prod" {
		logMode = logger.Warn
	}

	database, err := gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logMode),
	})
	if err != nil {
		slog.Error("failed to connect to the database", "error", err)
		return nil, err
	}

	if err := database.AutoMigrate(&User{}, &PublicKey{}, &Game{}, &Ranking{}); err != nil {
		slog.Error("failed to run database migrations", "error", err)
		return nil, err
	}

	return database, nil
}
