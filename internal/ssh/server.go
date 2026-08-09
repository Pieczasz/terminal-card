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

const (
	handshakeTimeout  = 20 * time.Second
	connIdleTimeout   = 30 * time.Minute
	maxTerminalWidth  = 2000
	maxTerminalHeight = 600
)

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
	Tracker         *SessionTracker
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
		wish.WithPublicKeyAuth(rateLimitAuth(rateLimiter, func(_ ssh.Context, _ ssh.PublicKey) bool {
			return true
		})),
		boundedPty(),
		// wish runs the last middleware first, so this slice is in reverse execution
		// order.
		wish.WithMiddleware(
			bm.MiddlewareWithProgramHandler(sessionProgram(deps, tracker)),
			activeterm.Middleware(),
			logging.StructuredMiddleware(),
			sessionLifecycle(deps, tracker),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("error while setting up wish ssh server: %w", err)
	}
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
		if !limiter.Allow(ratelimit.NetKey(host)) {
			observability.RateLimitRejectsTotal.Add(1)
			slog.Warn("rate limited ssh connection", "remote_addr", ctx.RemoteAddr().String(), "session_id", ctx.SessionID())
			return false
		}
		return next(ctx, key)
	}
}

func sessionTraceContext(s ssh.Session) context.Context {
	if ctx, ok := s.Context().Value(ctxKeyTraceCtx).(context.Context); ok {
		return ctx
	}
	return s.Context()
}

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

func boundedPty() ssh.Option {
	return func(srv *ssh.Server) error {
		srv.PtyCallback = func(_ ssh.Context, req ssh.Pty) bool {
			return req.Window.Width <= maxTerminalWidth && req.Window.Height <= maxTerminalHeight
		}
		return nil
	}
}

func sessionProgram(deps ServerDependencies, tracker *SessionTracker) bm.ProgramHandler {
	newModel := sessionModel(deps, tracker)
	return func(s ssh.Session) *tea.Program {
		model, opts := newModel(s)
		if model == nil {
			return nil
		}
		opts = append(opts, bm.MakeOptions(s)...)
		return tea.NewProgram(model, append(opts, tea.WithFilter(clampWindowSize))...)
	}
}

func clampWindowSize(_ tea.Model, msg tea.Msg) tea.Msg {
	switch msg := msg.(type) {
	case tea.SuspendMsg:
		return tea.ResumeMsg{}
	case tea.WindowSizeMsg:
		msg.Width = min(msg.Width, maxTerminalWidth)
		msg.Height = min(msg.Height, maxTerminalHeight)
		return msg
	}
	return msg
}

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
			defer recoverSession(s)
			defer releaseSession(s, deps, tracker)
			defer closeSessionModel(s)

			sh(s)
		}
	}
}

func recoverSession(s ssh.Session) {
	r := recover()
	if r == nil {
		return
	}
	slog.Error("critical panic recovered during ssh session",
		"panic", r,
		"remote_addr", s.RemoteAddr().String(),
	)
	wish.Fatalf(s, "\r\nAn unexpected internal error occurred. The administrators have been notified.\r\n")
}

func closeSessionModel(s ssh.Session) {
	if c, ok := s.Context().Value(ctxKeyModel).(interface{ Close() }); ok {
		c.Close()
	}
}

func releaseSession(s ssh.Session, deps ServerDependencies, tracker *SessionTracker) {
	if s.Context().Value(ctxKeyOwnsConnection) != true {
		return
	}
	if u, ok := s.Context().Value(ctxKeyUser).(*db.User); ok {
		tracker.Disconnect(u.ID)
		deps.LobbyManager.LeaveLobby(lobby.NewPlayer(u))
	}
}
