package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSessions int

func (f fakeSessions) Count() int { return int(f) }

type fakeLobbies struct{ inGame, waiting int }

func (f fakeLobbies) Stats() (int, int) { return f.inGame, f.waiting }

// stubUsers implements only what the handler calls; the rest panics so an accidental
// new call site shows up as a test failure rather than a silent zero value.
type stubUsers struct {
	db.UserRepository
	rankings []db.Ranking
	err      error
	gotLimit int
}

func (s *stubUsers) BestPlayers(_ context.Context, limit int) ([]db.Ranking, error) {
	s.gotLimit = limit
	return s.rankings, s.err
}

func ranking(name, game string, elo uint32) db.Ranking {
	return db.Ranking{
		Elo:  elo,
		User: db.User{Username: name},
		Game: db.Game{Name: game},
	}
}

func get(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.RemoteAddr = "203.0.113.7:5555"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestStats_ReportsLiveCounts(t *testing.T) {
	t.Parallel()
	h := Handler(Deps{
		Sessions: fakeSessions(4),
		Lobbies:  fakeLobbies{inGame: 2, waiting: 3},
	})

	rec := get(t, h, "/v1/stats")

	require.Equal(t, http.StatusOK, rec.Code)
	var got statsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, statsResponse{PlayersOnline: 4, HandsInPlay: 2, TablesOpen: 3}, got)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
}

// Nil dependencies must answer zeros rather than panic: the handler is mounted before
// anything guarantees every collaborator exists.
func TestStats_NilDepsDoNotPanic(t *testing.T) {
	t.Parallel()
	rec := get(t, Handler(Deps{}), "/v1/stats")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"players_online":0,"hands_in_play":0,"tables_open":0}`, rec.Body.String())
}

func TestLeaderboard_ShapesRanks(t *testing.T) {
	t.Parallel()
	users := &stubUsers{rankings: []db.Ranking{
		ranking("alice", "Poker", 1800),
		ranking("bob", "Poker", 1700),
	}}

	rec := get(t, Handler(Deps{Users: users}), "/v1/leaderboard")

	require.Equal(t, http.StatusOK, rec.Code)
	var got []leaderboardEntry
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got, 2)
	assert.Equal(t, leaderboardEntry{Rank: 1, Username: "alice", Game: "Poker", Elo: 1800}, got[0])
	assert.Equal(t, 2, got[1].Rank, "rank is the position, not anything from the row")
}

func TestLeaderboard_LimitIsClampedAndValidated(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		query     string
		wantCode  int
		wantLimit int
	}{
		{name: "absent uses the default", query: "", wantCode: http.StatusOK, wantLimit: defaultLimit},
		{name: "in range passes through", query: "?limit=10", wantCode: http.StatusOK, wantLimit: 10},
		{name: "over the cap is clamped", query: "?limit=1000", wantCode: http.StatusOK, wantLimit: maxLeaderboardLimit},
		{name: "zero is refused", query: "?limit=0", wantCode: http.StatusBadRequest},
		{name: "negative is refused", query: "?limit=-3", wantCode: http.StatusBadRequest},
		{name: "nonsense is refused", query: "?limit=all", wantCode: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			users := &stubUsers{}
			rec := get(t, Handler(Deps{Users: users}), "/v1/leaderboard"+tt.query)

			require.Equal(t, tt.wantCode, rec.Code)
			if tt.wantCode == http.StatusOK {
				assert.Equal(t, tt.wantLimit, users.gotLimit)
			}
		})
	}
}

// A database failure must not leak its message to an unauthenticated caller.
func TestLeaderboard_RepositoryErrorIsOpaque(t *testing.T) {
	t.Parallel()
	users := &stubUsers{err: errors.New("pq: relation \"rankings\" does not exist")}

	rec := get(t, Handler(Deps{Users: users}), "/v1/leaderboard")

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.NotContains(t, rec.Body.String(), "relation")
	assert.Contains(t, rec.Body.String(), "unavailable")
}

func TestLeaderboard_EmptyIsAnArrayNotNull(t *testing.T) {
	t.Parallel()
	// `null` would make the site's rows.slice(0,5) throw; an empty array renders the
	// "be first" message instead.
	rec := get(t, Handler(Deps{Users: &stubUsers{}}), "/v1/leaderboard")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "[]", rec.Body.String()[:2])
}

func TestUnknownRouteIs404(t *testing.T) {
	t.Parallel()
	assert.Equal(t, http.StatusNotFound, get(t, Handler(Deps{}), "/v1/secrets").Code)
	assert.Equal(t, http.StatusNotFound, get(t, Handler(Deps{}), "/").Code)
}

func TestWriteMethodsAreRejected(t *testing.T) {
	t.Parallel()
	h := Handler(Deps{Sessions: fakeSessions(1)})

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(method, "/v1/stats", nil)
			req.RemoteAddr = "203.0.113.9:1111"
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			assert.NotEqual(t, http.StatusOK, rec.Code, "%s must not be served", method)
		})
	}
}

func TestPreflightIsAnswered(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodOptions, "/v1/stats", nil)
	req.RemoteAddr = "203.0.113.11:2222"
	rec := httptest.NewRecorder()
	Handler(Deps{}).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Contains(t, rec.Header().Get("Access-Control-Allow-Methods"), "GET")
}

func TestRateLimitRejectsAFlood(t *testing.T) {
	t.Parallel()
	h := Handler(Deps{Sessions: fakeSessions(1), RequestsPerMinute: 3})

	var lastCode int
	for range 6 {
		lastCode = get(t, h, "/v1/stats").Code
	}

	assert.Equal(t, http.StatusTooManyRequests, lastCode, "a flood from one network is throttled")
}

func TestAllowOriginCanBePinned(t *testing.T) {
	t.Parallel()
	rec := get(t, Handler(Deps{AllowOrigin: "https://tty.cards"}), "/v1/stats")
	assert.Equal(t, "https://tty.cards", rec.Header().Get("Access-Control-Allow-Origin"))
}

// Behind a proxy every socket address is the proxy's, so without TrustedProxy the
// whole internet would share one bucket. With it, distinct clients get distinct ones.
func TestRateLimit_TrustedProxySeparatesClients(t *testing.T) {
	t.Parallel()
	h := Handler(Deps{Sessions: fakeSessions(1), RequestsPerMinute: 2, TrustedProxy: true})

	send := func(clientIP string) int {
		req := httptest.NewRequest(http.MethodGet, "/v1/stats", nil)
		req.RemoteAddr = "10.0.0.1:9999" // always the proxy
		req.Header.Set("X-Forwarded-For", clientIP)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	assert.Equal(t, http.StatusOK, send("198.51.100.1"))
	assert.Equal(t, http.StatusOK, send("198.51.100.1"))
	assert.Equal(t, http.StatusTooManyRequests, send("198.51.100.1"), "that client is spent")
	assert.Equal(t, http.StatusOK, send("203.0.113.200"), "a different client has its own budget")
}

// The header must be ignored unless explicitly trusted, or a directly exposed server
// could be evaded by forging it.
func TestRateLimit_UntrustedProxyHeaderIsIgnored(t *testing.T) {
	t.Parallel()
	h := Handler(Deps{Sessions: fakeSessions(1), RequestsPerMinute: 2})

	var last int
	for i := range 4 {
		req := httptest.NewRequest(http.MethodGet, "/v1/stats", nil)
		req.RemoteAddr = "198.51.100.9:1234"
		// A fresh forged address every time would grant unlimited budget if trusted.
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("203.0.113.%d", i+1))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		last = rec.Code
	}

	assert.Equal(t, http.StatusTooManyRequests, last, "forged headers must not reset the budget")
}

// X-Forwarded-For is a chain; the client is leftmost.
func TestTrustedProxy_UsesLeftmostForwardedAddress(t *testing.T) {
	t.Parallel()
	h := Handler(Deps{Sessions: fakeSessions(1), RequestsPerMinute: 1, TrustedProxy: true})

	send := func(xff string) int {
		req := httptest.NewRequest(http.MethodGet, "/v1/stats", nil)
		req.RemoteAddr = "10.0.0.1:9999"
		req.Header.Set("X-Forwarded-For", xff)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	require.Equal(t, http.StatusOK, send("198.51.100.5, 10.0.0.1"))
	assert.Equal(t, http.StatusTooManyRequests, send("198.51.100.5, 172.16.0.9"),
		"same client through a different hop is still the same client")
}
