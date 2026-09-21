package db_test

import (
	"testing"

	"github.com/Pieczasz/terminal-card/internal/config"
	"github.com/Pieczasz/terminal-card/internal/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// No container needed: the DSN cannot resolve, so this is a pure error path and
// belongs outside the integration build tag.
func TestConnect_Failure(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		DBHost:         "invalid-host",
		DBPort:         5432,
		DBUser:         "user",
		DBPassword:     "pass",
		DBName:         "db",
		DBSSLMode:      "disable",
		MaxConnections: 5,
	}

	database, err := db.Connect(cfg)
	require.Error(t, err)
	assert.Nil(t, database)
}
