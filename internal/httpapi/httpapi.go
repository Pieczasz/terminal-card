// Package httpapi serves the small read-only JSON feed the marketing site reads:
// how many people are connected, how many hands are in play, and the top of the
// leaderboard.
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/observability"
	"github.com/Pieczasz/terminal-card/internal/ratelimit"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	maxLeaderboardLimit = 200
	defaultLimit        = 5

	cacheSeconds = 15

	readTimeout  = 5 * time.Second
	writeTimeout = 10 * time.Second
	idleTimeout  = 60 * time.Second
)

type SessionCounter interface {
	Count() int
}

type LobbyCounter interface {
	Stats() (inGame, waiting int)
}

type Deps struct {
	Sessions SessionCounter
	Lobbies  LobbyCounter
	// Users is the concrete db interface while Sessions and Lobbies are local
	// one-method interfaces. That asymmetry is deliberate and shipped: the two
	// counters exist only to keep this package from importing ssh and lobby, whereas
	// db.UserRepository is already the contract every consumer depends on.
	Users db.UserRepository

	AllowOrigin       string
	RequestsPerMinute int
	TrustedProxy      bool

	// Health reports whether the process's dependencies are usable (the database
	// ping, in practice). nil means /healthz only asserts the process serves HTTP.
	Health func(ctx context.Context) error
}

type statsResponse struct {
	PlayersOnline int `json:"players_online"`
	HandsInPlay   int `json:"hands_in_play"`
	TablesOpen    int `json:"tables_open"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type leaderboardEntry struct {
	Rank     int    `json:"rank"`
	Username string `json:"username"`
	Game     string `json:"game"`
	Elo      uint32 `json:"elo"`
}

func Handler(deps Deps) http.Handler {
	perMinute := deps.RequestsPerMinute
	if perMinute <= 0 {
		perMinute = 120
	}
	limiter := ratelimit.NewSlidingWindowLimiter(perMinute, time.Minute)
	clientAddr := clientIPFunc(deps.TrustedProxy)

	mux := http.NewServeMux()
	mux.Handle("GET /v1/stats", statsHandler(deps))
	mux.Handle("GET /healthz", healthHandler(deps))
	mux.Handle("GET /v1/leaderboard", leaderboardHandler(deps))

	// The preflight answer is a route rather than a short-circuit in withCORS, so
	// that OPTIONS is spent against the same budget as every other request.
	mux.HandleFunc("OPTIONS /", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "not found")
	})

	return otelhttp.NewHandler(
		withCORS(deps.AllowOrigin, withRateLimit(limiter, clientAddr, mux)),
		"stats-api",
		otelhttp.WithSpanNameFormatter(routeSpanName),
	)
}

// routeSpanName names a span after the route, never after the raw path: anything
// else lets a caller mint unbounded span names by inventing URLs.
func routeSpanName(operation string, r *http.Request) string {
	switch r.URL.Path {
	case "/v1/stats", "/v1/leaderboard":
		return r.Method + " " + r.URL.Path
	default:
		return operation
	}
}

func statsHandler(deps Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inGame, waiting := 0, 0
		if deps.Lobbies != nil {
			inGame, waiting = deps.Lobbies.Stats()
		}
		online := 0
		if deps.Sessions != nil {
			online = deps.Sessions.Count()
		}
		writeJSON(w, r, statsResponse{
			PlayersOnline: online,
			HandsInPlay:   inGame,
			TablesOpen:    waiting,
		})
	})
}

func leaderboardHandler(deps Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := defaultLimit
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 {
				writeError(w, r, http.StatusBadRequest, "limit must be a positive integer")
				return
			}
			limit = min(n, maxLeaderboardLimit)
		}

		if deps.Users == nil {
			writeJSON(w, r, []leaderboardEntry{})
			return
		}

		rankings, err := deps.Users.BestPlayers(r.Context(), limit, r.URL.Query().Get("game"))
		if err != nil {
			slog.ErrorContext(r.Context(), "leaderboard query failed", "error", err)
			writeError(w, r, http.StatusServiceUnavailable, "leaderboard unavailable")
			return
		}

		out := make([]leaderboardEntry, 0, len(rankings))
		for i, entry := range rankings {
			out = append(out, leaderboardEntry{
				Rank:     i + 1,
				Username: entry.User.Username,
				Game:     entry.Game.Name,
				Elo:      entry.Elo,
			})
		}
		writeJSON(w, r, out)
	})
}

func clientIPFunc(trustProxy bool) func(*http.Request) string {
	socketHost := func(r *http.Request) string {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return r.RemoteAddr
		}
		return host
	}
	if !trustProxy {
		return socketHost
	}
	return func(r *http.Request) string {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			if first, _, found := strings.Cut(fwd, ","); found {
				return strings.TrimSpace(first)
			}
			return strings.TrimSpace(fwd)
		}
		return socketHost(r)
	}
}

func Serve(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: readTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

func writeJSON(w http.ResponseWriter, r *http.Request, v any) {
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", cacheSeconds))
	w.Header().Add("Vary", "Origin")
	encodeJSON(w, r, http.StatusOK, v)
}

// writeError replaces http.Error: a client that asked for JSON gets JSON back,
// including on the 429 and 503 paths it is most likely to have to parse.
func writeError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Add("Vary", "Origin")
	encodeJSON(w, r, status, errorResponse{Error: msg})
}

func encodeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)

	if r.Method == http.MethodHead {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.WarnContext(r.Context(), "failed to encode api response", "error", err, "path", r.URL.Path)
	}
}

// withCORS sets the Access-Control-Allow-Origin header. The default is "*" because
// this API is intentionally public and read-only. If any authenticated or
// write endpoint is ever added under this handler, the caller-supplied origin
// must be tightened to a specific allowlist — a wildcard on an authenticated
// origin defeats SameSite cookie protections.
func withCORS(origin string, next http.Handler) http.Handler {
	if origin == "" {
		origin = "*"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
		w.Header().Set("Access-Control-Max-Age", "86400")
		next.ServeHTTP(w, r)
	})
}

func withRateLimit(
	limiter *ratelimit.SlidingWindowLimiter,
	clientAddr func(*http.Request) string,
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow(ratelimit.NetKey(clientAddr(r))) {
			observability.RateLimitReject(r.Context(), "http")
			w.Header().Set("Retry-After", "60")
			writeError(w, r, http.StatusTooManyRequests, "too many requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// healthHandler backs the container healthcheck: 200 when the process serves and
// its dependencies answer, 503 otherwise, so orchestration restarts a wedged
// process instead of routing players into it.
func healthHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Health != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := deps.Health(ctx); err != nil {
				slog.ErrorContext(ctx, "health check failed", "error", err)
				writeError(w, r, http.StatusServiceUnavailable, "unhealthy")
				return
			}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}
}
