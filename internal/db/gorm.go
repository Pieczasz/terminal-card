package db

import (
	"fmt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"log/slog"
	"os"
	"strings"
)

func Connect() (*gorm.DB, error) {
	passwordBytes, err := os.ReadFile("/run/secrets/backend-password")
	var password string
	if err == nil {
		password = strings.TrimSpace(string(passwordBytes))
	} else {
		slog.Error("could not read secret file", "error", err)
		return nil, err
	}

	host := os.Getenv("DB_HOST")
	if host == "" {
		host = "db"
	}

	dsn := fmt.Sprintf("host=%s user=postgres password=%s dbname=terminal_card port=5432 sslmode=disable TimeZone=UTC", host, password)

	logMode := logger.Info
	if os.Getenv("ENV") == "prod" {
		logMode = logger.Warn
	}

	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logMode),
	})
	if err != nil {
		slog.Error("failed to connect to the database: %v", "error", err)
		return nil, err
	}

	if err := database.AutoMigrate(&User{}, &PublicKey{}); err != nil {
		slog.Error("failed to run database migrations:", "error", err)
		return nil, err
	}

	return database, nil
}
