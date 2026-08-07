// Package ssh contains implementation for setting up ssh auth, middleware, and
// server setup.
package ssh

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"github.com/Pieczasz/terminal-card/internal/config"
	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/lobby"
	"github.com/Pieczasz/terminal-card/internal/observability"
	"github.com/Pieczasz/terminal-card/internal/player"
	"github.com/Pieczasz/terminal-card/internal/ratelimit"
	"github.com/Pieczasz/terminal-card/internal/tui"

	tea "charm.land/bubbletea/v2"
	"charm.land/ssh"
	"charm.land/wish/v2"
	"charm.land/wish/v2/activeterm"
	bm "charm.land/wish/v2/bubbletea"
	"charm.land/wish/v2/logging"
	"github.com/charmbracelet/keygen"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ctxKey namespaces ssh.Context values to avoid collision with other middleware.
type ctxKey int

const (
	ctxKeyOwnsConnection ctxKey = iota
	ctxKeyUser
	ctxKeyTraceCtx
	ctxKeyModel
)

// handshakeTimeout bounds how long a connection may stay unauthenticated.
//
// Until a client attempts auth it is invisible to rateLimitAuth, yet it already
// holds one of the MAX_CONNECTIONS listener slots - so without this, opening that
// many sockets and never speaking again locks every real player out. charm.land/ssh
// drops the deadline once the handshake succeeds and idleTimeout takes over.
const handshakeTimeout = 20 * time.Second

// connIdleTimeout is deliberately far looser than the router's five-minute TUI
// idle check: it reaps connections that went away without a FIN, and is not the
// gameplay rule. A game exempts itself from the TUI check, so anything tighter
// here would drop players waiting on a slow table.
const connIdleTimeout = 30 * time.Minute

type SessionTracker struct {
	mu     sync.Mutex
	active map[uint]bool
}

func NewSessionTracker() *SessionTracker {
	return &SessionTracker{
		active: make(map[uint]bool),
	}
}

func (t *SessionTracker) Connect(userID uint) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.active[userID] {
		return false
	}
	t.active[userID] = true
	observability.SSHSessionsActive.Add(1)
	return true
}

// Count reports how many distinct users hold a live session. Read by the public
// stats endpoint, so it must not block a connect or disconnect.
func (t *SessionTracker) Count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.active)
}

func (t *SessionTracker) Disconnect(userID uint) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.active[userID]; ok {
		delete(t.active, userID)
		observability.SSHSessionsActive.Add(-1)
	}
}

type ServerDependencies struct {
	Config          *config.Config
	UserRepository  db.UserRepository
	MatchRepository db.MatchRepository
	LobbyManager    *lobby.Manager
	GameRegistry    *game.Registry
	// Tracker is optional. Supply one when something outside the ssh server needs
	// to read the live session count (the stats endpoint does); leave it nil and
	// SetupServer owns a private one.
	Tracker *SessionTracker
}

func SetupServer(deps ServerDependencies) (*ssh.Server, error) {
	key, err := keygen.New(deps.Config.SSHKeyPath, keygen.WithKeyType(keygen.Ed25519))
	if err != nil {
		return nil, fmt.Errorf("generating a keygen pair error: %w", err)
	}

	if !key.KeyPairExists() {
		if err := key.WriteKeys(); err != nil {
			return nil, fmt.Errorf("error while saving keypair to disk: %w", err)
		}
	}
	if err := ensureHostKeyPermissions(deps.Config.SSHKeyPath); err != nil {
		return nil, err
	}

	tracker := deps.Tracker
	if tracker == nil {
		tracker = NewSessionTracker()
	}
	rateLimiter := ratelimit.NewSlidingWindowLimiter(deps.Config.RateLimitCount, deps.Config.RateLimitWindow)

	server, err := wish.NewServer(
		wish.WithAddress(fmt.Sprintf("%s:%d", deps.Config.ServerHost, deps.Config.ServerPort)),
		wish.WithHostKeyPEM(key.RawPrivateKey()),
		wish.WithIdleTimeout(connIdleTimeout),
		// Any key is accepted; identity is bound to its fingerprint in LoadOrRegisterUser.
		wish.WithPublicKeyAuth(rateLimitAuth(rateLimiter, func(_ ssh.Context, _ ssh.PublicKey) bool {
			return true
		})),
		// wish runs the LAST middleware first, so this slice is in reverse execution
		// order. sessionLifecycle is listed last to be outermost; see its doc.
		wish.WithMiddleware(
			bm.Middleware(sessionModel(deps, tracker)),
			activeterm.Middleware(),
			logging.StructuredMiddleware(),
			sessionLifecycle(deps, tracker),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("error while setting up wish ssh server: %w", err)
	}
	// wish exposes no option for this one, and it has to be set before Serve.
	server.HandshakeTimeout = handshakeTimeout

	return server, nil
}

func ensureHostKeyPermissions(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat ssh host key: %w", err)
	}
	mode := info.Mode().Perm()
	if mode&0o077 != 0 {
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("chmod ssh host key: %w", err)
		}
	}
	return nil
}

func rateLimitAuth(limiter *ratelimit.SlidingWindowLimiter, next ssh.PublicKeyHandler) ssh.PublicKeyHandler {
	return func(ctx ssh.Context, key ssh.PublicKey) bool {
		host, _, err := net.SplitHostPort(ctx.RemoteAddr().String())
		if err != nil {
			host = ctx.RemoteAddr().String()
		}
		// Budgets are held against the client's network, not its exact address:
		// see ratelimit.NetKey for why a per-address limit is meaningless over IPv6.
		if !limiter.Allow(ratelimit.NetKey(host)) {
			observability.RateLimitRejectsTotal.Add(1)
			slog.Warn("rate limited ssh connection", "remote_addr", ctx.RemoteAddr().String(), "session_id", ctx.SessionID())
			return false
		}
		return next(ctx, key)
	}
}

// sessionTraceContext returns the context carrying the session span (set by the
// outer middleware), so downstream DB calls and logs join the session trace.
func sessionTraceContext(s ssh.Session) context.Context {
	if ctx, ok := s.Context().Value(ctxKeyTraceCtx).(context.Context); ok {
		return ctx
	}
	return s.Context()
}

// sessionModel authenticates the session and builds the TUI for it, rejecting the
// connection if the user is already connected elsewhere. Returning a nil model
// after wish.Fatalf is how this middleware refuses a session.
func sessionModel(deps ServerDependencies, tracker *SessionTracker) func(ssh.Session) (tea.Model, []tea.ProgramOption) {
	return func(s ssh.Session) (tea.Model, []tea.ProgramOption) {
		traceCtx := sessionTraceContext(s)
		fingerprint, err := AuthenticateSession(s)
		if err != nil {
			wish.Fatalf(s, "%v\n", err)
			return nil, nil
		}
		user, err := LoadOrRegisterUser(traceCtx, deps.UserRepository, s.User(), fingerprint)
		if err != nil {
			wish.Fatalf(s, "%v\n", err)
			return nil, nil
		}
		if !tracker.Connect(user.ID) {
			wish.Fatalf(s, "Account '%s' is already connected from another session.\n", user.Username)
			return nil, nil
		}

		s.Context().SetValue(ctxKeyOwnsConnection, true)
		s.Context().SetValue(ctxKeyUser, user)

		// Stash the model so sessionLifecycle can tear it down once the program
		// returns; a disconnect never runs the views' own exit paths.
		model := tui.Model(tui.ModelDependencies{
			SessionCtx:   traceCtx,
			User:         *user,
			UserRepo:     deps.UserRepository,
			MatchRepo:    deps.MatchRepository,
			LobbyManager: deps.LobbyManager,
			GameRegistry: deps.GameRegistry,
		})
		s.Context().SetValue(ctxKeyModel, model)

		return model, []tea.ProgramOption{}
	}
}

// sessionLifecycle spans the session, recovers panics, and releases everything the
// session held. It is the outermost middleware so it wraps the bubbletea program:
// charm.land/ssh runs the handler in a goroutine with no recover, so an escaped
// panic would crash the whole process, and cleanup must run on every disconnect.
func sessionLifecycle(deps ServerDependencies, tracker *SessionTracker) wish.Middleware {
	return func(sh ssh.Handler) ssh.Handler {
		return func(s ssh.Session) {
			ctx, span := otel.Tracer("terminal-card/ssh").Start(s.Context(), "ssh.session",
				trace.WithAttributes(attribute.String("remote_addr", s.RemoteAddr().String())))
			s.Context().SetValue(ctxKeyTraceCtx, ctx)
			defer func() {
				if u, ok := s.Context().Value(ctxKeyUser).(*db.User); ok {
					span.SetAttributes(attribute.String("user", u.Username))
				}
				span.End()
			}()
			defer releaseSession(s, deps, tracker)

			sh(s)
		}
	}
}

// releaseSession runs on every disconnect, panic or not.
//
// It must stay deferred directly (defer releaseSession(...)): recover only stops a
// panic when called by the deferred function itself, so wrapping this in another
// helper would silently let panics escape and take the process down.
func releaseSession(s ssh.Session, deps ServerDependencies, tracker *SessionTracker) {
	if r := recover(); r != nil {
		slog.Error("critical panic recovered during ssh session",
			"panic", r,
			"remote_addr", s.RemoteAddr().String(),
		)
		wish.Fatalf(s, "\r\nAn unexpected internal error occurred. The administrators have been notified.\r\n")
	}

	// Release event subscriptions the active view still holds, or a mid-game
	// disconnect parks its listener goroutine and keeps a broadcaster slot until
	// the engine itself closes.
	if c, ok := s.Context().Value(ctxKeyModel).(interface{ Close() }); ok {
		c.Close()
	}

	if s.Context().Value(ctxKeyOwnsConnection) != true {
		return
	}
	if u, ok := s.Context().Value(ctxKeyUser).(*db.User); ok {
		tracker.Disconnect(u.ID)
		deps.LobbyManager.LeaveLobby(&player.Player{ID: fmt.Sprint(u.ID), DatabaseUser: u})
	}
}
