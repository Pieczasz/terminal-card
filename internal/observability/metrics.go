package observability

import "sync/atomic"

var (
	SSHSessionsActive     atomic.Int64
	GamesStartedTotal     atomic.Int64
	RateLimitRejectsTotal atomic.Int64
)
