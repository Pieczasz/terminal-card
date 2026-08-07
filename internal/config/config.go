package config

import (
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Env                  string
	ServerHost           string
	ServerPort           int
	APIPort              int
	APIAllowOrigin       string
	APITrustProxy        bool
	MaxConnections       int
	SSHKeyPath           string
	DBHost               string
	DBPort               int
	DBUser               string
	DBName               string
	DBPassword           string
	DBSSLMode            string
	DBMaxOpenConnections int
	OTelEndpoint         string
	OTelInsecure         bool
	ServiceVersion       string
	RateLimitCount       int
	RateLimitWindow      time.Duration
}

// intEnvs accumulates integer env lookups so one error check covers all of them
// instead of one per variable. The first failure is the one reported.
type intEnvs struct {
	err error
}

func (e *intEnvs) get(key string, fallback int) int {
	value, err := getEnvAsInt(key, fallback)
	if err != nil && e.err == nil {
		e.err = fmt.Errorf("invalid %s: %w", key, err)
	}
	return value
}

// resolveEnv reads ENV, loads .env outside production, and falls back to
// development for any unrecognised value.
func resolveEnv() string {
	env := getEnv("ENV", "development")
	if env != "production" {
		_ = godotenv.Load()
	}
	switch env {
	case "production", "development", "staging":
		return env
	default:
		return "development"
	}
}

// otelInsecure defaults to plaintext outside production, overridable by env.
func otelInsecure(env string) bool {
	if v, ok := os.LookupEnv("OTEL_EXPORTER_OTLP_INSECURE"); ok && v != "" {
		return v == "1" || v == "true" || v == "TRUE"
	}
	return env != "production"
}

func Load() (*Config, error) {
	env := resolveEnv()

	ints := &intEnvs{}
	serverPort := ints.get("SERVER_PORT", 6969)
	apiPort := ints.get("API_PORT", 6970)
	maxConnections := ints.get("MAX_CONNECTIONS", 1000)
	dbPort := ints.get("DB_PORT", 5432)
	dbMaxOpenConnections := ints.get("DB_MAX_OPEN_CONNS", 25)
	rateLimitCount := ints.get("RATE_LIMIT_CONNECTIONS", 5)
	rateLimitWindowMS := ints.get("RATE_LIMIT_WINDOW_MS", 1000)
	if ints.err != nil {
		return nil, ints.err
	}

	defaultSSLMode := "disable"
	if env == "production" {
		defaultSSLMode = "require"
	}

	cfg := &Config{
		Env:            env,
		ServerHost:     getEnv("SERVER_HOST", "0.0.0.0"),
		ServerPort:     serverPort,
		APIPort:        apiPort,
		APIAllowOrigin: getEnv("API_ALLOW_ORIGIN", "*"),
		// The compose deployment never publishes the API port, so the only
		// possible source of X-Forwarded-For is the nginx in front of it.
		APITrustProxy:        getEnv("API_TRUST_PROXY", "true") == "true",
		MaxConnections:       maxConnections,
		SSHKeyPath:           getEnv("SSH_KEY_PATH", ".wishlist/server"),
		DBHost:               getEnv("DB_HOST", "localhost"),
		DBPort:               dbPort,
		DBUser:               getEnv("DB_USER", "postgres"),
		DBName:               getEnv("DB_NAME", "terminal_card"),
		DBPassword:           getEnv("DB_PASSWORD", ""),
		DBSSLMode:            getEnv("DB_SSLMODE", defaultSSLMode),
		DBMaxOpenConnections: dbMaxOpenConnections,
		OTelEndpoint:         getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317"),
		OTelInsecure:         otelInsecure(env),
		ServiceVersion:       getEnv("SERVICE_VERSION", detectVersion()),
		RateLimitCount:       rateLimitCount,
		RateLimitWindow:      time.Duration(rateLimitWindowMS) * time.Millisecond,
	}

	if cfg.DBSSLMode == "" {
		cfg.DBSSLMode = defaultSSLMode
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
		return errors.New("DB_PASSWORD is required when ENV=production")
	}
	if c.Env == "production" && c.DBSSLMode == "disable" {
		allowInsecure := os.Getenv("ALLOW_INSECURE_DB") == "true"
		internalHost := c.DBHost == "db" || c.DBHost == "localhost" || c.DBHost == "127.0.0.1"
		if !allowInsecure && !internalHost {
			return fmt.Errorf("DB_SSLMODE=disable is not allowed in production for host %q; "+
				"set ALLOW_INSECURE_DB=true only for trusted networks", c.DBHost)
		}
	}
	if c.RateLimitCount < 1 {
		return errors.New("RATE_LIMIT_CONNECTIONS must be at least 1")
	}
	if c.RateLimitWindow < time.Millisecond {
		return errors.New("RATE_LIMIT_WINDOW_MS must be at least 1")
	}
	if c.DBMaxOpenConnections < 1 {
		return errors.New("DB_MAX_OPEN_CONNS must be at least 1")
	}
	return nil
}

func (c *Config) DSN() string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=%s TimeZone=UTC",
		c.DBHost, c.DBUser, c.DBPassword, c.DBName, c.DBPort, c.DBSSLMode)
}

func detectVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "0.1.0"
}

// getEnv returns the environment value for key, falling back when it is unset or
// empty. An empty value must not win: a blank SSH_KEY_PATH or DB_HOST in a .env or
// compose file would otherwise silently defeat the default. This matches
// getEnvAsInt, which already treats "" as absent.
func getEnv(key string, fallback string) string {
	if value, exists := os.LookupEnv(key); exists && value != "" {
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
