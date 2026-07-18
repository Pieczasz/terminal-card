package config

import (
	"fmt"
	"net/url"
	"os"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Env             string
	ServerHost      string
	ServerPort      int
	MaxConnections  int
	SSHKeyPath      string
	DBHost          string
	DBPort          int
	DBUser          string
	DBName          string
	DBPassword      string
	DBSslMode       string
	DBMaxOpenConns  int
	OTelEndpoint    string
	OTelInsecure    bool
	ServiceVersion  string
	RateLimitCount  int
	RateLimitWindow time.Duration
}

func Load() (*Config, error) {
	// Attempt to load .env file if it exists and we are not explicitly in production
	if os.Getenv("ENV") != "production" {
		_ = godotenv.Load()
	}

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

	dbMaxOpenConns, err := getEnvAsInt("DB_MAX_OPEN_CONNS", 25)
	if err != nil {
		return nil, fmt.Errorf("invalid DB_MAX_OPEN_CONNS: %w", err)
	}

	rateLimitCount, err := getEnvAsInt("RATE_LIMIT_CONNECTIONS", 5)
	if err != nil {
		return nil, fmt.Errorf("invalid RATE_LIMIT_CONNECTIONS: %w", err)
	}

	rateLimitWindowMS, err := getEnvAsInt("RATE_LIMIT_WINDOW_MS", 1000)
	if err != nil {
		return nil, fmt.Errorf("invalid RATE_LIMIT_WINDOW_MS: %w", err)
	}

	defaultSslMode := "disable"
	if env == "production" {
		defaultSslMode = "require"
	}

	otelInsecure := env != "production"
	if v, ok := os.LookupEnv("OTEL_EXPORTER_OTLP_INSECURE"); ok && v != "" {
		otelInsecure = v == "1" || v == "true" || v == "TRUE"
	}

	cfg := &Config{
		Env:             env,
		ServerHost:      getEnv("SERVER_HOST", "0.0.0.0"),
		ServerPort:      serverPort,
		MaxConnections:  maxConnections,
		SSHKeyPath:      getEnv("SSH_KEY_PATH", ".wishlist/server"),
		DBHost:          getEnv("DB_HOST", "localhost"),
		DBPort:          dbPort,
		DBUser:          getEnv("DB_USER", "postgres"),
		DBName:          getEnv("DB_NAME", "terminal_card"),
		DBPassword:      getEnv("DB_PASSWORD", ""),
		DBSslMode:       getEnv("DB_SSLMODE", defaultSslMode),
		DBMaxOpenConns:  dbMaxOpenConns,
		OTelEndpoint:    getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317"),
		OTelInsecure:    otelInsecure,
		ServiceVersion:  getEnv("SERVICE_VERSION", detectVersion()),
		RateLimitCount:  rateLimitCount,
		RateLimitWindow: time.Duration(rateLimitWindowMS) * time.Millisecond,
	}

	if cfg.DBSslMode == "" {
		cfg.DBSslMode = defaultSslMode
	}
	if cfg.ServiceVersion == "" {
		cfg.ServiceVersion = detectVersion()
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// Validate checks production-critical configuration.
func (c *Config) Validate() error {
	if c.Env == "production" && c.DBPassword == "" {
		return fmt.Errorf("DB_PASSWORD is required when ENV=production")
	}
	if c.Env == "production" && c.DBSslMode == "disable" {
		allowInsecure := os.Getenv("ALLOW_INSECURE_DB") == "true"
		internalHost := c.DBHost == "db" || c.DBHost == "localhost" || c.DBHost == "127.0.0.1"
		if !allowInsecure && !internalHost {
			return fmt.Errorf("DB_SSLMODE=disable is not allowed in production for host %q; set ALLOW_INSECURE_DB=true only for trusted networks", c.DBHost)
		}
	}
	if c.RateLimitCount < 1 {
		return fmt.Errorf("RATE_LIMIT_CONNECTIONS must be at least 1")
	}
	if c.RateLimitWindow < time.Millisecond {
		return fmt.Errorf("RATE_LIMIT_WINDOW_MS must be at least 1")
	}
	if c.DBMaxOpenConns < 1 {
		return fmt.Errorf("DB_MAX_OPEN_CONNS must be at least 1")
	}
	return nil
}

func (c *Config) DSN() string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=%s TimeZone=UTC",
		c.DBHost, c.DBUser, c.DBPassword, c.DBName, c.DBPort, c.DBSslMode)
}

// MigrateDSN returns a postgres URL suitable for golang-migrate.
func (c *Config) MigrateDSN() string {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.DBUser, c.DBPassword),
		Host:   fmt.Sprintf("%s:%d", c.DBHost, c.DBPort),
		Path:   "/" + c.DBName,
	}
	q := u.Query()
	q.Set("sslmode", c.DBSslMode)
	u.RawQuery = q.Encode()
	return u.String()
}

func detectVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "0.1.0"
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
