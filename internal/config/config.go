package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Env              string
	ServerPort       int
	SSHKeyPath       string
	DBHost           string
	DBPort           int
	DBUser           string
	DBName           string
	DBPassword       string
	DBPasswordFile   string
}

func Load() (*Config, error) {
	// Attempt to load .env file if it exists (mostly for local development)
	// It's okay if it fails (e.g. in production using standard env vars or docker compose)
	_ = godotenv.Load()

	cfg := &Config{
		Env:              getEnv("ENV", "development"),
		ServerPort:       getEnvAsInt("SERVER_PORT", 6969),
		SSHKeyPath:       getEnv("SSH_KEY_PATH", ".wishlist/server"),
		DBHost:           getEnv("DB_HOST", "localhost"),
		DBPort:           getEnvAsInt("DB_PORT", 5432),
		DBUser:           getEnv("DB_USER", "postgres"),
		DBName:           getEnv("DB_NAME", "terminal_card"),
		DBPassword:       getEnv("DB_PASSWORD", ""),
		DBPasswordFile:   getEnv("DB_PASSWORD_FILE", ""),
	}

	// Read password from file if the file is provided (e.g. Docker secrets)
	if cfg.DBPasswordFile != "" {
		passwordBytes, err := os.ReadFile(cfg.DBPasswordFile)
		if err == nil {
			cfg.DBPassword = strings.TrimSpace(string(passwordBytes))
		}
	}

	return cfg, nil
}

// DSN generates the PostgreSQL connection string
func (c *Config) DSN() string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=disable TimeZone=UTC",
		c.DBHost, c.DBUser, c.DBPassword, c.DBName, c.DBPort)
}

func getEnv(key string, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getEnvAsInt(name string, fallback int) int {
	valStr := getEnv(name, "")
	if val, err := strconv.Atoi(valStr); err == nil {
		return val
	}
	return fallback
}
