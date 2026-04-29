package main

import (
	"client/internal/db"
	"client/internal/ssh"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	database, err := setupDatabase()
	if err != nil {
		slog.Error("failed to setup database: %v", err)
		panic(err)
	}

	server, err := ssh.SetupServer(database)
	if err != nil {
		slog.Error("error while setting up the server: %v", err)
		panic(err)
	}

	if err := server.ListenAndServe(); err != nil {
		slog.Error("server: starting ssh server error: %v", err)
		panic(err)
	}
}

func setupDatabase() (*gorm.DB, error) {
	passwordBytes, err := os.ReadFile("/run/secrets/backend-password")
	var password string
	if err == nil {
		password = strings.TrimSpace(string(passwordBytes))
	} else {
		slog.Error("could not read secret file %v", err)
		return nil, err
	}

	host := os.Getenv("DB_HOST")
	if host == "" {
		host = "db"
	}

	dsn := fmt.Sprintf("host=%s user=postgres password=%s dbname=terminal_card port=5432 sslmode=disable TimeZone=UTC", host, password)

	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		slog.Error("failed to connect to the database: %v", err)
		return nil, err
	}

	if err := database.AutoMigrate(&db.User{}, &db.PublicKey{}); err != nil {
		slog.Error("failed to run database migrations: %v", err)
		return nil, err
	}

	return database, nil
}
