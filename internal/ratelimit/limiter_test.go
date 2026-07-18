package ratelimit_test

import (
	"testing"
	"time"

	"terminalcard/internal/ratelimit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlidingWindowLimiter_Allow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		limit    int
		window   time.Duration
		requests int
		wantLast bool
		wantSize int
	}{
		{
			name:     "under limit",
			limit:    3,
			window:   time.Second,
			requests: 2,
			wantLast: true,
			wantSize: 1,
		},
		{
			name:     "at limit then deny",
			limit:    2,
			window:   time.Second,
			requests: 3,
			wantLast: false,
			wantSize: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			limiter := ratelimit.NewSlidingWindowLimiter(tt.limit, tt.window)

			var last bool
			for i := 0; i < tt.requests; i++ {
				last = limiter.Allow("1.2.3.4")
			}
			assert.Equal(t, tt.wantLast, last)
			assert.Equal(t, tt.wantSize, limiter.Size())
		})
	}
}

func TestSlidingWindowLimiter_IndependentIPs(t *testing.T) {
	t.Parallel()
	limiter := ratelimit.NewSlidingWindowLimiter(1, time.Second)

	require.True(t, limiter.Allow("10.0.0.1"))
	require.False(t, limiter.Allow("10.0.0.1"))
	require.True(t, limiter.Allow("10.0.0.2"))
	assert.Equal(t, 2, limiter.Size())
}

func TestSlidingWindowLimiter_WindowExpiryEvicts(t *testing.T) {
	t.Parallel()
	limiter := ratelimit.NewSlidingWindowLimiter(1, 20*time.Millisecond)

	require.True(t, limiter.Allow("1.1.1.1"))
	require.False(t, limiter.Allow("1.1.1.1"))
	assert.Equal(t, 1, limiter.Size())

	time.Sleep(25 * time.Millisecond)

	require.True(t, limiter.Allow("1.1.1.1"), "same IP should be allowed after window expiry")
	assert.Equal(t, 1, limiter.Size())
}
