// Package httpapi serves the small read-only JSON feed the marketing site reads:
// how many people are connected, how many hands are in play, and the top of the
// leaderboard.
//
// This is deliberately not "the API". It exposes no writes, no auth, no per-user
// data, and nothing the leaderboard screen inside the TUI does not already show any
// visitor. Keeping it that narrow is what makes it safe to publish unauthenticated.
package httpapi

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/ratelimit"
)

const (
	// maxLeaderboardLimit caps what a caller may ask for, so a single request cannot
	// turn into a hundred-row render on every page load.
	maxLeaderboardLimit = 25
	defaultLimit        = 5

	// Responses are cheap but not free: BestPlayers is cached for five minutes
	// inside the repository, and this keeps intermediaries from asking that often.
	cacheSeconds = 15

	readTimeout  = 5 * time.Second
	writeTimeout = 10 * time.Second
	idleTimeout  = 60 * time.Second
)

// SessionCounter reports live SSH sessions. Implemented by ssh.SessionTracker.
type SessionCounter interface {
	Count() int
}

// LobbyCounter reports lobbies by state. Implemented by lobby.Manager.
type LobbyCounter interface {
	Stats() (inGame, waiting int)
}

// Deps is everything the handler reads. Narrow interfaces rather than the concrete
// types keep this package free of imports from ssh and lobby, and make it testable
// without standing either of them up.
type Deps struct {
	Sessions SessionCounter
	Lobbies  LobbyCounter
	Users    db.UserRepository

	// AllowOrigin is sent as Access-Control-Allow-Origin. Empty means "*", which is
	// correct here: every response is public, and no request carries credentials, so
	// a permissive origin grants a caller nothing they could not get by fetching the
	// page directly. Set it if you would rather pin the site's origin anyway.
	AllowOrigin string

	// RequestsPerMinute throttles per client network. Zero picks a sane default.
	RequestsPerMinute int

	// TrustedProxy reads the client address from X-Forwarded-For instead of the
	// socket. Behind a reverse proxy every request appears to come from the proxy,
	// so without this the whole internet shares one rate-limit bucket.
	//
	// Only enable it when this listener is unreachable except through that proxy.
	// A directly exposed server that trusts the header lets any caller forge an
	// address and evade the limit completely.
	TrustedProxy bool
}

type statsResponse struct {
	PlayersOnline int `json:"players_online"`
	HandsInPlay   int `json:"hands_in_play"`
	TablesOpen    int `json:"tables_open"`
}

type leaderboardEntry struct {
	Rank     int    `json:"rank"`
	Username string `json:"username"`
	Game     string `json:"game"`
	Elo      uint32 `json:"elo"`
}

// Handler builds the router. Mount it under /api on whatever proxy fronts it.
func Handler(deps Deps) http.Handler {
	perMinute := deps.RequestsPerMinute
	if perMinute <= 0 {
		perMinute = 120
	}
	limiter := ratelimit.NewSlidingWindowLimiter(perMinute, time.Minute)
	clientAddr := clientIPFunc(deps.TrustedProxy)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/stats", func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("GET /v1/leaderboard", func(w http.ResponseWriter, r *http.Request) {
		limit := defaultLimit
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 {
				http.Error(w, `{"error":"limit must be a positive integer"}`, http.StatusBadRequest)
				return
			}
			limit = min(n, maxLeaderboardLimit)
		}

		if deps.Users == nil {
			writeJSON(w, r, []leaderboardEntry{})
			return
		}

		rankings, err := deps.Users.BestPlayers(r.Context(), limit)
		if err != nil {
			// The cause goes to the log, not to the client: a database error message
			// is internal detail.
			slog.Error("leaderboard query failed", "error", err)
			http.Error(w, `{"error":"leaderboard unavailable"}`, http.StatusServiceUnavailable)
			return
		}

		out := make([]leaderboardEntry, 0, len(rankings))
		for i, r := range rankings {
			out = append(out, leaderboardEntry{
				Rank:     i + 1,
				Username: r.User.Username,
				Game:     r.Game.Name,
				Elo:      r.Elo,
			})
		}
		writeJSON(w, r, out)
	})

	// Anything else, including a bare /, is not part of the contract.
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	})

	return withCORS(deps.AllowOrigin, withRateLimit(limiter, clientAddr, mux))
}

// Serve runs the API until ctx-driven shutdown is invoked by the caller. It returns
// the server so the caller owns Shutdown, matching how the ssh server is handled.
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
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", cacheSeconds))
	// Nothing here is user-specific, but the header stops a shared cache from
	// serving one origin's CORS response to another.
	w.Header().Add("Vary", "Origin")

	if r.Method == http.MethodHead {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("failed to encode api response", "error", err, "path", r.URL.Path)
	}
}

func withCORS(origin string, next http.Handler) http.Handler {
	if origin == "" {
		origin = "*"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
		w.Header().Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIPFunc returns how to identify a caller. Split out so the trust decision is
// made once, at construction, rather than re-read per request.
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
		// Leftmost entry is the original client; the rest is the proxy chain.
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			if first, _, found := strings.Cut(fwd, ","); found {
				return strings.TrimSpace(first)
			}
			return strings.TrimSpace(fwd)
		}
		return socketHost(r)
	}
}

func withRateLimit(
	limiter *ratelimit.SlidingWindowLimiter,
	clientAddr func(*http.Request) string,
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Budgets are held against the client's network rather than its exact
		// address, for the reason ratelimit.NetKey documents.
		if !limiter.Allow(ratelimit.NetKey(clientAddr(r))) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, `{"error":"too many requests"}`, http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
