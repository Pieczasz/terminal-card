package ratelimit

import (
	"slices"
	"sync"
	"time"
)

const defaultMaxKeys = 10_000

type SlidingWindowLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	logs    map[string][]time.Time
	ops     uint64
	maxKeys int
	now     func() time.Time
}

func NewSlidingWindowLimiter(limit int, window time.Duration) *SlidingWindowLimiter {
	return &SlidingWindowLimiter{
		limit:   limit,
		window:  window,
		logs:    make(map[string][]time.Time),
		maxKeys: defaultMaxKeys,
		now:     time.Now,
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

	now := s.now()
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

	_, exists := s.logs[ip]
	if !exists && len(s.logs) >= s.maxKeys {
		s.evictExpiredLocked(threshold)
		s.evictLeastRecentLocked()
	}

	s.logs[ip] = append(timestamps, now)

	s.ops++
	if s.ops%64 == 0 {
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

// evictLeastRecentLocked removes the key whose most-recent request is oldest.
// This is a best-effort defence against the maxKeys cap being reached under a
// distributed attack: an adversary with many IPs can rotate old entries out,
// effectively resetting their per-IP counters. The cap still bounds memory
// usage, which is its primary purpose. If the attack surface widens, raise
// maxKeys or switch to a shared store (Redis) that is not per-process.
func (s *SlidingWindowLimiter) evictLeastRecentLocked() {
	var victim string
	var oldest time.Time
	for ip, timestamps := range s.logs {
		if len(timestamps) == 0 {
			delete(s.logs, ip)
			continue
		}
		last := timestamps[len(timestamps)-1]
		if victim == "" || last.Before(oldest) {
			victim, oldest = ip, last
		}
	}
	if len(s.logs) >= s.maxKeys && victim != "" {
		delete(s.logs, victim)
	}
}

func (s *SlidingWindowLimiter) Size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.logs)
}
