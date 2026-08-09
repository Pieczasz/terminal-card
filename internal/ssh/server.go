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

// maxTerminalWidth and maxTerminalHeight bound the geometry a client may claim its
// terminal has. Both numbers arrive from the client as uint32s in pty-req and
// window-change, and every frame the TUI lays out is allocated from them, so one
// request for four billion columns is a remote out-of-memory.
//
// The two differ because the plausible extremes do. A very wide display at a very
// small font can genuinely exceed a thousand columns; nothing has close to a
// thousand rows. Bounding them separately keeps the worst-case buffer near what a
// single square limit already allowed while refusing no real terminal.
const (
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
		boundedPty(),
		// wish runs the LAST middleware first, so this slice is in reverse execution
		// order. sessionLifecycle is listed last to be outermost; see its doc.
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

// boundedPty refuses a pty-req claiming a geometry no terminal has. Rejecting is the
// only lever the callback has: it is handed the request by value, so it cannot
// rewrite it.
//
// It must stay a rejection rather than becoming a clamp in clampWindowSize, which
// looks like the tidier fix and is not. bubbletea seeds the renderer by calling
// renderer.resize() directly with the initial size (tea.Program.Run), and only the
// copy it *also* sends as a WindowSizeMsg goes through tea.WithFilter. So the first
// allocation happens from the client's raw numbers no matter what the filter does,
// and this callback is the last place they can still be refused.
func boundedPty() ssh.Option {
	return func(srv *ssh.Server) error {
		srv.PtyCallback = func(_ ssh.Context, req ssh.Pty) bool {
			return req.Window.Width <= maxTerminalWidth && req.Window.Height <= maxTerminalHeight
		}
		return nil
	}
}

// sessionProgram builds the session's program itself so the window-size filter is
// installed last: bm.MakeOptions ends with a filter of its own and tea keeps only
// the final one, so bm.Middleware would silently drop ours.
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

// clampWindowSize bounds a resize before any view lays out from it. It also answers
// SuspendMsg, which is the job of the filter it replaces: there is no shell behind an
// ssh session to suspend into.
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

// sessionLifecycle spans the session, recovers panics, and releases everything the
// session held. It is the outermost middleware so it wraps the bubbletea program,
// and cleanup must run on every disconnect.
//
// The three defers below are separate on purpose and all direct, because recover only
// sees a panic unwinding the function it was deferred from. Registration order is
// reverse execution order, so the model is closed first, the session is released next
// - even while a panic from that close is unwinding - and recoverSession catches
// whatever is left before it reaches the handler goroutine.
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

// recoverSession contains a panic from the session, whether it came from the program
// or from the cleanup deferred after it. It must stay deferred directly
// (defer recoverSession(s)): recover only stops a panic when it is called by the
// deferred function itself.
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

// closeSessionModel releases event subscriptions the active view still holds, or a
// mid-game disconnect parks its listener goroutine and keeps a broadcaster slot until
// the engine itself closes.
//
// It is its own defer so that a view whose Close panics still cannot cost the player
// the release below: that is what refuses their next login.
func closeSessionModel(s ssh.Session) {
	if c, ok := s.Context().Value(ctxKeyModel).(interface{ Close() }); ok {
		c.Close()
	}
}

// releaseSession hands back the session slot and the lobby seat on every disconnect,
// panic or not.
func releaseSession(s ssh.Session, deps ServerDependencies, tracker *SessionTracker) {
	if s.Context().Value(ctxKeyOwnsConnection) != true {
		return
	}
	if u, ok := s.Context().Value(ctxKeyUser).(*db.User); ok {
		tracker.Disconnect(u.ID)
		deps.LobbyManager.LeaveLobby(lobby.NewPlayer(u))
	}
}
