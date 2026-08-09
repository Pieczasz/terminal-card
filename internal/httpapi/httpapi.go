// Package httpapi serves the small read-only JSON feed the marketing site reads:
// how many people are connected, how many hands are in play, and the top of the
// leaderboard.
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
	Users    db.UserRepository

	AllowOrigin       string
	RequestsPerMinute int
	TrustedProxy      bool
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
			slog.Error("leaderboard query failed", "error", err)
			writeError(w, r, http.StatusServiceUnavailable, "leaderboard unavailable")
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

	// The preflight answer is a route rather than a short-circuit in withCORS, so
	// that OPTIONS is spent against the same budget as every other request.
	mux.HandleFunc("OPTIONS /", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "not found")
	})

	return withCORS(deps.AllowOrigin, withRateLimit(limiter, clientAddr, mux))
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
			w.Header().Set("Retry-After", "60")
			writeError(w, r, http.StatusTooManyRequests, "too many requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}
