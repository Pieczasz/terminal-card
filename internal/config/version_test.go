package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		want    string
	}{
		{name: "an unstamped build falls back", version: "", want: fallbackVersion},
		{name: "a local build falls back", version: "(devel)", want: fallbackVersion},
		{name: "a real tag is kept", version: "v1.2.3", want: "v1.2.3"},
		{name: "a pseudo-version is kept", version: "v0.0.0-20240101120000-abcdef123456", want: "v0.0.0-20240101120000-abcdef123456"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, normalizeVersion(tt.version))
		})
	}
}

func TestDetectVersion(t *testing.T) {
	t.Parallel()
	assert.NotEmpty(t, detectVersion(), "a version always resolves to something")
}
