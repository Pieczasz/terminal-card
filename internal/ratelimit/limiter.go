package ratelimit

import (
	"sync"
	"time"
)

type SlidingWindowLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	logs   map[string][]time.Time
	ops    uint64
}

func NewSlidingWindowLimiter(limit int, window time.Duration) *SlidingWindowLimiter {
	return &SlidingWindowLimiter{
		limit:  limit,
		window: window,
		logs:   make(map[string][]time.Time),
	}
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

	s.logs[ip] = append(timestamps, now)

	s.ops++
	if s.ops%64 == 0 {
		s.evictExpiredLocked(threshold)
	}
	return true
}

func filterExpired(timestamps []time.Time, threshold time.Time) []time.Time {
	if len(timestamps) == 0 {
		return nil
	}
	valid := make([]time.Time, 0, len(timestamps))
	for _, t := range timestamps {
		if t.After(threshold) {
			valid = append(valid, t)
		}
	}
	return valid
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

// Size returns the number of tracked IP keys (for tests and metrics).
func (s *SlidingWindowLimiter) Size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.logs)
}
