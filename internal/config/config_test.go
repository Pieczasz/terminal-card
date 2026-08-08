package config_test

import (
	"os"
	"path/filepath"
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

// An env var that is present but blank must not defeat the default.
func TestLoad_BlankEnvFallsBackToDefault(t *testing.T) {
	blanked := []string{
		"SERVER_HOST", "SSH_KEY_PATH", "DB_HOST", "DB_USER", "DB_NAME",
		"DB_SSLMODE", "OTEL_EXPORTER_OTLP_ENDPOINT", "SERVICE_VERSION",
	}
	t.Setenv("ENV", "development")
	for _, key := range blanked {
		t.Setenv(key, "")
	}

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0", cfg.ServerHost)
	assert.Equal(t, ".wishlist/server", cfg.SSHKeyPath)
	assert.Equal(t, "localhost", cfg.DBHost)
	assert.Equal(t, "postgres", cfg.DBUser)
	assert.Equal(t, "terminal_card", cfg.DBName)
	assert.Equal(t, "disable", cfg.DBSSLMode)
	assert.Equal(t, "localhost:4317", cfg.OTelEndpoint)
	assert.NotEmpty(t, cfg.ServiceVersion, "version always resolves to something")
}

// Trusting X-Forwarded-For on a directly reachable listener lets any caller forge an
// X-Forwarded-For on a directly reachable listener can be forged, so off is the only
// safe default and turning it on takes an explicit opt-in.
func TestLoad_TrustProxyDefaultsToOff(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "unset", value: "", want: false},
		{name: "explicitly false", value: "false", want: false},
		{name: "anything else is not an opt-in", value: "1", want: false},
		{name: "yes is not an opt-in either", value: "yes", want: false},
		{name: "opted in", value: "true", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ENV", "development")
			t.Setenv("API_TRUST_PROXY", tt.value)

			cfg, err := config.Load()
			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.APITrustProxy)
		})
	}
}

// An explicitly configured SSL mode has to survive Load: silently replacing it with the
// environment default would either weaken or break a deployment.
func TestLoad_ExplicitSSLModeIsHonored(t *testing.T) {
	t.Setenv("ENV", "production")
	t.Setenv("DB_PASSWORD", "secret")
	t.Setenv("DB_SSLMODE", "verify-full")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "verify-full", cfg.DBSSLMode)
}

// .env is a development convenience.
func TestResolveEnv_DotEnvLoadedOutsideProductionOnly(t *testing.T) {
	tests := []struct {
		name       string
		env        string
		wantDBName string
	}{
		{name: "development reads .env", env: "development", wantDBName: "from_dotenv"},
		{name: "staging reads .env", env: "staging", wantDBName: "from_dotenv"},
		{name: "production ignores .env", env: "production", wantDBName: "terminal_card"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("DB_NAME=from_dotenv\n"), 0o600))
			t.Chdir(dir)

			t.Setenv("ENV", tt.env)
			t.Setenv("DB_PASSWORD", "secret")
			// godotenv never overrides a variable that is already present, so this one
			// has to be genuinely absent for the file to be observable at all.
			t.Setenv("DB_NAME", "")
			require.NoError(t, os.Unsetenv("DB_NAME"))

			cfg, err := config.Load()
			require.NoError(t, err)
			assert.Equal(t, tt.wantDBName, cfg.DBName)
			assert.Equal(t, tt.env, cfg.Env)
		})
	}
}

// An unrecognised ENV is not a reason to guess production behavior.
func TestResolveEnv_UnknownFallsBackToDevelopment(t *testing.T) {
	t.Setenv("ENV", "wat")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "development", cfg.Env)
}

// Each of these limits is "at least one", so one itself has to pass.
func TestValidate_LowestAllowedValuesAreValid(t *testing.T) {
	t.Parallel()

	base := func() *config.Config {
		return &config.Config{
			Env:                  "development",
			RateLimitCount:       1,
			RateLimitWindow:      time.Millisecond,
			DBMaxOpenConnections: 1,
		}
	}

	require.NoError(t, base().Validate(), "the documented minimum of each limit is valid")

	tests := []struct {
		name    string
		breakIt func(*config.Config)
		want    string
	}{
		{name: "no connections allowed", breakIt: func(c *config.Config) { c.RateLimitCount = 0 }, want: "RATE_LIMIT_CONNECTIONS"},
		{name: "sub-millisecond window", breakIt: func(c *config.Config) { c.RateLimitWindow = time.Microsecond }, want: "RATE_LIMIT_WINDOW_MS"},
		{name: "no db connections", breakIt: func(c *config.Config) { c.DBMaxOpenConnections = 0 }, want: "DB_MAX_OPEN_CONNS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := base()
			tt.breakIt(cfg)

			err := cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

// String is the only thing standing between a careless log line and the database password
// ending up in log storage, so it is checked directly.
func TestConfig_String_RedactsThePassword(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{DBUser: "postgres", DBHost: "db", DBPassword: "super-secret-value"}

	rendered := cfg.String()

	assert.NotContains(t, rendered, "super-secret-value", "the password must never be formatted")
	assert.Contains(t, rendered, "[REDACTED]")
	assert.Contains(t, rendered, "postgres", "everything else still needs to be readable")
	assert.Equal(t, "super-secret-value", cfg.DBPassword, "the config itself is not mutated")
}

func TestConfig_String_HandlesTheEmptyCases(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "<nil>", (*config.Config)(nil).String())
	assert.NotContains(t, (&config.Config{}).String(), "[REDACTED]", "no password, nothing to redact")
}

// DSN still carries the password; that is what the driver needs.
func TestConfig_DSN_CarriesThePasswordAndStringDoesNot(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{DBHost: "db", DBUser: "u", DBPassword: "pw", DBName: "n", DBPort: 5432, DBSSLMode: "require"}

	assert.Contains(t, cfg.DSN(), "password=pw")
	assert.NotContains(t, cfg.String(), "pw")
}
