package ratelimit_test

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/ratelimit"

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

	require.Eventually(t, func() bool {
		return limiter.Allow("1.1.1.1")
	}, 200*time.Millisecond, 5*time.Millisecond)
	assert.Equal(t, 1, limiter.Size())
}

func TestSlidingWindowLimiter_MaxKeys(t *testing.T) {
	t.Parallel()
	limiter := ratelimit.NewSlidingWindowLimiter(1, time.Minute).WithMaxKeys(2)

	require.True(t, limiter.Allow("a"))
	require.True(t, limiter.Allow("b"))
	require.False(t, limiter.Allow("c"), "new key should be rejected when at capacity")
	require.False(t, limiter.Allow("a"), "existing key still rate-limited")
}

func BenchmarkAllow(b *testing.B) {
	for _, keys := range []int{1, 64, 10_000} {
		b.Run(fmt.Sprintf("keys=%d", keys), func(b *testing.B) {
			l := ratelimit.NewSlidingWindowLimiter(1_000_000, time.Minute)
			ids := make([]string, keys)
			for i := range keys {
				ids[i] = strconv.Itoa(i)
				l.Allow(ids[i])
			}

			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				l.Allow(ids[i%keys])
			}
		})
	}
}

// A non-positive cap would refuse every caller the limiter has not seen before, so it has
// to be ignored rather than applied.
func TestSlidingWindowLimiter_NonPositiveMaxKeysIsIgnored(t *testing.T) {
	t.Parallel()

	for _, n := range []int{0, -1} {
		limiter := ratelimit.NewSlidingWindowLimiter(1, time.Minute).WithMaxKeys(n)

		assert.Truef(t, limiter.Allow("1.1.1.1"), "WithMaxKeys(%d) must keep the default cap", n)
	}
}

// Entries for callers who have gone quiet are swept periodically, not only when the key cap
// is reached: without it a burst of one-off addresses stays resident for as long as the process runs.
func TestSlidingWindowLimiter_SweepsExpiredKeysPeriodically(t *testing.T) {
	t.Parallel()
	// The sweep runs every 64th call, so the window has to be short enough that the
	// first 63 callers have expired by the time the 64th arrives.
	const window = 20 * time.Millisecond
	limiter := ratelimit.NewSlidingWindowLimiter(1, window)

	for i := range 63 {
		require.True(t, limiter.Allow(strconv.Itoa(i)))
	}
	require.Equal(t, 63, limiter.Size(), "nothing is swept before the sweep is due")

	time.Sleep(4 * window)
	require.True(t, limiter.Allow("late"), "the 64th call is the one that sweeps")

	assert.Equal(t, 1, limiter.Size(), "every caller that has gone quiet is dropped")
}
