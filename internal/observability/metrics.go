package observability

import "sync/atomic"

// Process-local counters for self-host operators. These are cheap to increment
// and can be scraped later via an exporter if needed.
var (
	SSHSessionsActive     atomic.Int64
	GamesStartedTotal     atomic.Int64
	RateLimitRejectsTotal atomic.Int64
)
