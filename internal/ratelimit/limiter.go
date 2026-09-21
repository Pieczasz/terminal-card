package ratelimit

import (
	"slices"
	"sync"
	"time"
)

const (
	defaultMaxKeys = 10_000
	// sweepEvery amortises the only full-table walk left over that many calls.
	sweepEvery = 64
)

type SlidingWindowLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	logs    map[string][]time.Time
	ops     uint64
	maxKeys int
}

func NewSlidingWindowLimiter(limit int, window time.Duration) *SlidingWindowLimiter {
	return &SlidingWindowLimiter{
		limit:   limit,
		window:  window,
		logs:    make(map[string][]time.Time),
		maxKeys: defaultMaxKeys,
	}
}

// WithMaxKeys caps the number of tracked keys to bound memory under abuse.
func (s *SlidingWindowLimiter) WithMaxKeys(n int) *SlidingWindowLimiter {
	if n > 0 {
		s.maxKeys = n
	}
	return s
}

func (s *SlidingWindowLimiter) Allow(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	threshold := now.Add(-s.window)

	timestamps := filterExpired(s.logs[ip], threshold)
	if len(timestamps) == 0 {
		delete(s.logs, ip)
	} else {
		s.logs[ip] = timestamps
	}

	if len(timestamps) >= s.limit {
		return false
	}

	if _, exists := s.logs[ip]; !exists && len(s.logs) >= s.maxKeys {
		s.evictOneLocked()
	}

	s.logs[ip] = append(timestamps, now)

	s.ops++
	if s.ops%sweepEvery == 0 {
		s.evictExpiredLocked(threshold)
	}
	return true
}

// filterExpired drops timestamps at or before threshold in place. Both callers
// reassign the result, so reusing the backing array costs nothing and keeps the hot
// path allocation-free.
func filterExpired(timestamps []time.Time, threshold time.Time) []time.Time {
	return slices.DeleteFunc(timestamps, func(t time.Time) bool { return !t.After(threshold) })
}

func (s *SlidingWindowLimiter) evictExpiredLocked(threshold time.Time) {
	for ip, timestamps := range s.logs {
		valid := filterExpired(timestamps, threshold)
		if len(valid) == 0 {
			delete(s.logs, ip)
		} else {
			s.logs[ip] = valid
		}
	}
}

// evictOneLocked drops a single key in map order, which is O(1) and is the whole
// point: a full table is reached by a flood of fresh addresses (one IPv6 /48 has
// 65536 networks to spend), and picking the "best" victim meant walking all
// maxKeys entries under the mutex on every one of those requests - the flood paid
// for with our CPU. Go randomizes map iteration, so this is an arbitrary victim,
// not LRU; during a flood any eviction is cheaper than walking the table.
func (s *SlidingWindowLimiter) evictOneLocked() {
	for ip := range s.logs {
		delete(s.logs, ip)
		return
	}
}

func (s *SlidingWindowLimiter) Size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.logs)
}
