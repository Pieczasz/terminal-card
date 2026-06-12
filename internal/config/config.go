package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Env            string
	ServerHost     string
	ServerPort     int
	MaxConnections int
	SSHKeyPath     string
	DBHost         string
	DBPort         int
	DBUser         string
	DBName         string
	DBPassword     string
	DBSslMode      string
	OTelEndpoint   string
}

func Load() (*Config, error) {
	// Attempt to load .env file if it exists
	_ = godotenv.Load()

	env := getEnv("ENV", "development")
	if env != "production" && env != "development" {
		env = "development"
	}

	serverPort, err := getEnvAsInt("SERVER_PORT", 6969)
	if err != nil {
		return nil, fmt.Errorf("invalid SERVER_PORT: %w", err)
	}

	maxConnections, err := getEnvAsInt("MAX_CONNECTIONS", 1000)
	if err != nil {
		return nil, fmt.Errorf("invalid MAX_CONNECTIONS: %w", err)
	}

	dbPort, err := getEnvAsInt("DB_PORT", 5432)
	if err != nil {
		return nil, fmt.Errorf("invalid DB_PORT: %w", err)
	}

	defaultSslMode := "disable"
	if env == "production" {
		defaultSslMode = "require"
	}

	cfg := &Config{
		Env:            env,
		ServerHost:     getEnv("SERVER_HOST", "0.0.0.0"),
		ServerPort:     serverPort,
		MaxConnections: maxConnections,
		SSHKeyPath:     getEnv("SSH_KEY_PATH", ".wishlist/server"),
		DBHost:         getEnv("DB_HOST", "localhost"),
		DBPort:         dbPort,
		DBUser:         getEnv("DB_USER", "postgres"),
		DBName:         getEnv("DB_NAME", "terminal_card"),
		DBPassword:     getEnv("DB_PASSWORD", ""),
		DBSslMode:      getEnv("DB_SSLMODE", defaultSslMode),
		OTelEndpoint:   getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317"),
	}

	return cfg, nil
}

func (c *Config) DSN() string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=%s TimeZone=UTC",
		c.DBHost, c.DBUser, c.DBPassword, c.DBName, c.DBPort, c.DBSslMode)
}

func getEnv(key string, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getEnvAsInt(name string, fallback int) (int, error) {
	valStr := getEnv(name, "")
	if valStr == "" {
		return fallback, nil
	}
	val, err := strconv.Atoi(valStr)
	if err != nil {
		return 0, fmt.Errorf("trying to parse env value failed: %w", err)
	}
	return val, nil
}
