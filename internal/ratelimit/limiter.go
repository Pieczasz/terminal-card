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

	timestamps := s.logs[ip]

	var validTimestamps []time.Time
	for _, t := range timestamps {
		if t.After(threshold) {
			validTimestamps = append(validTimestamps, t)
		}
	}

	if len(validTimestamps) >= s.limit {
		s.logs[ip] = validTimestamps
		return false
	}

	validTimestamps = append(validTimestamps, now)
	s.logs[ip] = validTimestamps
	return true
}
