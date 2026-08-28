package observability

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// SSHSessionsActive stays an atomic behind an observable gauge: it is a
// dimensionless level, which is the one shape that pattern fits. Everything else
// is a synchronous instrument below, because those carry attributes.
var SSHSessionsActive atomic.Int64

// meter uses the otel global, which delegates: instruments created before
// SetupOTel installs the real provider start recording once it does.
var meter = otel.Meter("terminal-card")

// mustCounter panics only on a malformed instrument name, which is a compile-time
// class of mistake; the otel global never fails for provider reasons.
func mustCounter(name, desc string) metric.Int64Counter {
	c, err := meter.Int64Counter(name, metric.WithDescription(desc))
	if err != nil {
		panic(fmt.Sprintf("observability: create counter %s: %v", name, err))
	}
	return c
}

func mustHistogram(name, desc, unit string) metric.Float64Histogram {
	h, err := meter.Float64Histogram(name,
		metric.WithDescription(desc), metric.WithUnit(unit))
	if err != nil {
		panic(fmt.Sprintf("observability: create histogram %s: %v", name, err))
	}
	return h
}

// Attribute values here are bounded sets only (game names, small enums, route
// patterns) - never a user ID, session ID, or address, which belong on spans
// and log lines where cardinality is free.
var (
	sshSessions = mustCounter("terminalcard.ssh.sessions",
		"SSH connection outcomes")
	sshSessionDuration = mustHistogram("terminalcard.ssh.session.duration",
		"SSH session duration", "s")
	sshPanics = mustCounter("terminalcard.ssh.session.panics",
		"Panics recovered during SSH sessions")
	rateLimitRejects = mustCounter("terminalcard.ratelimit.rejects",
		"Requests rejected by a rate limiter")
	gamesStarted = mustCounter("terminalcard.games.started",
		"Games started")
	gamesFinished = mustCounter("terminalcard.games.finished",
		"Games finished")
	gameDuration = mustHistogram("terminalcard.game.duration",
		"Wall-clock duration of a game", "s")
	turnTimeouts = mustCounter("terminalcard.game.turn.timeouts",
		"Turns played by the clock instead of the player")
	idleRemovals = mustCounter("terminalcard.game.players.idle_removed",
		"Seats taken for idling")
	actionsRejected = mustCounter("terminalcard.game.action.rejected",
		"Player actions the rules refused")
	matchFinalize = mustCounter("terminalcard.match.finalize",
		"Match persistence attempts by outcome")
	broadcastDrops = mustCounter("terminalcard.broadcaster.events.dropped",
		"Events dropped by slow subscribers")
	subscribeFailures = mustCounter("terminalcard.broadcaster.subscribe.failures",
		"Event feed subscriptions refused")
	lobbyJoins = mustCounter("terminalcard.lobby.joins",
		"Lobby join attempts by outcome")
	lobbyTimeToStart = mustHistogram("terminalcard.lobby.time_to_start",
		"Time from lobby creation to game start", "s")
)

func SSHSession(ctx context.Context, outcome string) {
	sshSessions.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
}

func SSHSessionEnded(ctx context.Context, d time.Duration, outcome string) {
	sshSessionDuration.Record(ctx, d.Seconds(),
		metric.WithAttributes(attribute.String("outcome", outcome)))
}

func SSHPanicRecovered(ctx context.Context) {
	sshPanics.Add(ctx, 1)
}

func RateLimitReject(ctx context.Context, limiter string) {
	rateLimitRejects.Add(ctx, 1, metric.WithAttributes(attribute.String("limiter", limiter)))
}

func GameStarted(ctx context.Context, gameType string, ranked bool) {
	gamesStarted.Add(ctx, 1, metric.WithAttributes(
		attribute.String("game_type", gameType), attribute.Bool("ranked", ranked)))
}

func GameFinished(ctx context.Context, gameType string, ranked bool, reason string, d time.Duration) {
	attrs := metric.WithAttributes(
		attribute.String("game_type", gameType),
		attribute.Bool("ranked", ranked),
		attribute.String("reason", reason))
	gamesFinished.Add(ctx, 1, attrs)
	gameDuration.Record(ctx, d.Seconds(), attrs)
}

func TurnTimedOut(ctx context.Context, gameType string) {
	turnTimeouts.Add(ctx, 1, metric.WithAttributes(attribute.String("game_type", gameType)))
}

func PlayerIdleRemoved(ctx context.Context, gameType string) {
	idleRemovals.Add(ctx, 1, metric.WithAttributes(attribute.String("game_type", gameType)))
}

func ActionRejected(ctx context.Context, gameType string) {
	actionsRejected.Add(ctx, 1, metric.WithAttributes(attribute.String("game_type", gameType)))
}

// MatchFinalize records a persistence attempt. outcome "dropped" and "error" are
// the alertable ones: either means a finished match will not be in the database.
func MatchFinalize(ctx context.Context, outcome string, ranked bool) {
	matchFinalize.Add(ctx, 1, metric.WithAttributes(
		attribute.String("outcome", outcome), attribute.Bool("ranked", ranked)))
}

func BroadcastDropped(ctx context.Context, stream string, n int64) {
	broadcastDrops.Add(ctx, n, metric.WithAttributes(attribute.String("stream", stream)))
}

func SubscribeFailure(ctx context.Context, stream string) {
	subscribeFailures.Add(ctx, 1, metric.WithAttributes(attribute.String("stream", stream)))
}

func LobbyJoin(ctx context.Context, outcome string) {
	lobbyJoins.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
}

func LobbyStarted(ctx context.Context, gameType string, waited time.Duration) {
	lobbyTimeToStart.Record(ctx, waited.Seconds(),
		metric.WithAttributes(attribute.String("game_type", gameType)))
}

// RegisterDBStats exposes the connection pool as gauges. The pool is a hard cap
// that queues silently, so this is the only early warning before saturation.
func RegisterDBStats(db *sql.DB) error {
	inUse, err := meter.Int64ObservableGauge("db.client.connections.used",
		metric.WithDescription("Connections currently in use"))
	if err != nil {
		return fmt.Errorf("create pool gauge: %w", err)
	}
	idle, err := meter.Int64ObservableGauge("db.client.connections.idle",
		metric.WithDescription("Idle connections in the pool"))
	if err != nil {
		return fmt.Errorf("create idle gauge: %w", err)
	}
	waits, err := meter.Int64ObservableCounter("db.client.connections.wait_count",
		metric.WithDescription("Times a query waited for a free connection"))
	if err != nil {
		return fmt.Errorf("create wait counter: %w", err)
	}
	_, err = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		s := db.Stats()
		o.ObserveInt64(inUse, int64(s.InUse))
		o.ObserveInt64(idle, int64(s.Idle))
		o.ObserveInt64(waits, s.WaitCount)
		return nil
	}, inUse, idle, waits)
	if err != nil {
		return fmt.Errorf("register pool callback: %w", err)
	}
	return nil
}
