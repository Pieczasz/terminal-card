package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"client/internal/db"
	"client/internal/ssh"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	passwordBytes, err := os.ReadFile("/run/secrets/backend-password")
	var password string
	if err == nil {
		password = strings.TrimSpace(string(passwordBytes))
	} else {
		log.Fatalf("Could not read secret file %v", err)
	}

	host := os.Getenv("DB_HOST")
	if host == "" {
		host = "db"
	}

	dsn := fmt.Sprintf("host=%s user=postgres password=%s dbname=example port=5432 sslmode=disable TimeZone=UTC", host, password)

	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect to the database: %v", err)
	}
	log.Println("Successfully connected to the database.")

	if err := database.AutoMigrate(&db.User{}, &db.PublicKey{}); err != nil {
		log.Fatalf("failed to run database migrations: %v", err)
	}

	server, err := ssh.SetupServer(database)
	if err != nil {
		log.Fatalf("error while setting up the server: %v", err)
	}

	log.Printf("Starting SSH server...")
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("server: starting ssh server error: %v", err)
	}
}
