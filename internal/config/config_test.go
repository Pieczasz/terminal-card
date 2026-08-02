package config_test

import (
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("ENV", "development")
	t.Setenv("DB_PASSWORD", "")
	t.Setenv("SERVER_PORT", "")
	t.Setenv("MAX_CONNECTIONS", "")
	t.Setenv("DB_PORT", "")
	t.Setenv("DB_MAX_OPEN_CONNS", "")
	t.Setenv("RATE_LIMIT_CONNECTIONS", "")
	t.Setenv("RATE_LIMIT_WINDOW_MS", "")
	t.Setenv("DB_SSLMODE", "")
	t.Setenv("SERVER_HOST", "")
	t.Setenv("DB_HOST", "")
	t.Setenv("DB_USER", "")
	t.Setenv("DB_NAME", "")
	t.Setenv("SSH_KEY_PATH", "")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "")
	t.Setenv("SERVICE_VERSION", "")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "development", cfg.Env)
	assert.Equal(t, 6969, cfg.ServerPort)
	assert.Equal(t, "disable", cfg.DBSSLMode)
	assert.Equal(t, 5, cfg.RateLimitCount)
	assert.Equal(t, time.Second, cfg.RateLimitWindow)
	assert.Equal(t, 25, cfg.DBMaxOpenConnections)
	assert.True(t, cfg.OTelInsecure)
	assert.NotEmpty(t, cfg.ServiceVersion)
}

func TestLoad_ProductionRequiresPassword(t *testing.T) {
	t.Setenv("ENV", "production")
	t.Setenv("DB_PASSWORD", "")
	t.Setenv("DB_SSLMODE", "disable")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DB_PASSWORD")
}

func TestLoad_ProductionSSLDefault(t *testing.T) {
	t.Setenv("ENV", "production")
	t.Setenv("DB_PASSWORD", "secret")
	t.Setenv("DB_SSLMODE", "")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "require", cfg.DBSSLMode)
}

func TestLoad_InvalidPort(t *testing.T) {
	t.Setenv("ENV", "development")
	t.Setenv("SERVER_PORT", "not-a-number")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SERVER_PORT")
}

func TestValidate_RateLimit(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Env:             "development",
		RateLimitCount:  0,
		RateLimitWindow: time.Second,
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RATE_LIMIT_CONNECTIONS")
}

func TestDSN(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		DBHost:     "localhost",
		DBUser:     "postgres",
		DBPassword: "secret",
		DBName:     "terminal_card",
		DBPort:     5432,
		DBSSLMode:  "disable",
	}
	assert.Contains(t, cfg.DSN(), "host=localhost")
	assert.Contains(t, cfg.DSN(), "dbname=terminal_card")
	assert.Contains(t, cfg.DSN(), "sslmode=disable")
}

func TestValidate_InsecureDBRemoteHost(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_DB", "")
	cfg := &config.Config{
		Env:                  "production",
		DBPassword:           "secret",
		DBHost:               "db.example.com",
		DBSSLMode:            "disable",
		RateLimitCount:       5,
		RateLimitWindow:      time.Second,
		DBMaxOpenConnections: 25,
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DB_SSLMODE=disable")
}
