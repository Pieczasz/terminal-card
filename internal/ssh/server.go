// Package ssh contains implementation for setting up ssh auth, middleware, and
// server setup.
package ssh

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"sync/atomic"
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
	"github.com/charmbracelet/keygen"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// ctxKey namespaces ssh.Context values to avoid collision with other middleware.
type ctxKey int

// ctxKeyChannelCount is the only thing that belongs on the ssh.Context: charm ssh
// hands one Context per TCP connection to every channel opened on it, which is
// exactly the scope a per-connection counter wants. Everything else a session needs
// lives in sessionState, keyed by the session itself.
const ctxKeyChannelCount ctxKey = iota

const (
	handshakeTimeout  = 20 * time.Second
	connIdleTimeout   = 30 * time.Minute
	maxTerminalWidth  = 2000
	maxTerminalHeight = 600
	// maxSessionsPerConnection bounds concurrent session channels on one connection.
	// Every channel loads the user with three preloads against a small connection
	// pool, so an unbounded client could exhaust the database from a single TCP
	// connection. Two allows the reconnect overlap a real client produces.
	maxSessionsPerConnection = 2
)

// sessionState is per-channel session state. It cannot live on the ssh.Context: that
// is shared by every channel of the connection, so a rejected second channel's
// teardown would close the first channel's model and free its tracker slot. Fields
// are written and read from the one goroutine that runs the session handler chain.
type sessionState struct {
	traceCtx context.Context
	span     trace.Span
	started  time.Time
	user     *db.User
	model    interface{ Close() }
	owns     bool
	panicked bool
}

// sessionStates maps a live ssh.Session to its state. sessionLifecycle creates the
// entry and deletes it last; sessionModel and the teardown defers look it up.
var sessionStates sync.Map

func lookupSessionState(s ssh.Session) (*sessionState, bool) {
	st, ok := sessionStates.Load(s)
	if !ok {
		return nil, false
	}
	return st.(*sessionState), true
}

// ErrAlreadyConnected and ErrServerFull are Connect's refusals; the caller turns
// each into a different message, because "come back later" and "you are already
// here" call for different player action.
var (
	ErrAlreadyConnected = errors.New("account is already connected")
	ErrServerFull       = errors.New("server is at capacity")
)

type SessionTracker struct {
	mu     sync.Mutex
	active map[uint]bool
	// maxSessions is the player-visible capacity: Connect refuses beyond it with a
	// message, unlike the TCP-level LimitListener, which silently stops accepting.
	// Zero means unlimited.
	maxSessions int
}

func NewSessionTracker(maxSessions int) *SessionTracker {
	return &SessionTracker{
		active:      make(map[uint]bool),
		maxSessions: maxSessions,
	}
}

func (t *SessionTracker) Connect(userID uint) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.active[userID] {
		return ErrAlreadyConnected
	}
	if t.maxSessions > 0 && len(t.active) >= t.maxSessions {
		return ErrServerFull
	}
	t.active[userID] = true
	observability.SSHSessionsActive.Add(1)
	return nil
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
		tracker = NewSessionTracker(deps.Config.MaxConnections)
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
		// order. Connect/disconnect logging is sessionLifecycle's job rather than
		// wish's logging middleware: that one writes through the charm logger, which
		// bypasses slog and so never reaches the OTLP handler.
		wish.WithMiddleware(
			bm.MiddlewareWithProgramHandler(sessionProgram(deps, tracker)),
			activeterm.Middleware(),
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
			observability.RateLimitReject(ctx, "ssh")
			observability.SSHSession(ctx, "rejected_ratelimit")
			slog.WarnContext(ctx, "rate limited ssh connection",
				"remote_addr", ctx.RemoteAddr().String(), "session_id", ctx.SessionID())
			return false
		}
		return next(ctx, key)
	}
}

func sessionTraceContext(s ssh.Session) context.Context {
	if st, ok := lookupSessionState(s); ok && st.traceCtx != nil {
		return st.traceCtx
	}
	return s.Context()
}

// failSession reports a refusal on the session span as well as to the client, so a
// trace shows why a connection never got a screen.
func failSessionf(s ssh.Session, outcome string, err error, format string, args ...any) {
	ctx := sessionTraceContext(s)
	if st, ok := lookupSessionState(s); ok && st.span != nil {
		st.span.RecordError(err)
		st.span.SetStatus(codes.Error, outcome)
	}
	observability.SSHSession(ctx, outcome)
	wish.Fatalf(s, format, args...)
}

func sessionModel(deps ServerDependencies, tracker *SessionTracker) func(ssh.Session) (tea.Model, []tea.ProgramOption) {
	return func(s ssh.Session) (tea.Model, []tea.ProgramOption) {
		traceCtx := sessionTraceContext(s)
		fingerprint, err := AuthenticateSession(s)
		if err != nil {
			failSessionf(s, "auth_failed", err, "%v\n", err)
			return nil, nil
		}
		user, err := LoadOrRegisterUser(traceCtx, deps.UserRepository, s.User(), fingerprint)
		if err != nil {
			failSessionf(s, "auth_failed", err, "%v\n", err)
			return nil, nil
		}
		switch err := tracker.Connect(user.ID); {
		case errors.Is(err, ErrAlreadyConnected):
			failSessionf(s, "rejected_duplicate", err,
				"Account '%s' is already connected from another session.\n", user.Username)
			return nil, nil
		case errors.Is(err, ErrServerFull):
			failSessionf(s, "rejected_full", err,
				"The server is full right now - please try again in a few minutes.\n")
			return nil, nil
		case err != nil:
			failSessionf(s, "rejected", err, "%v\n", err)
			return nil, nil
		}
		observability.SSHSession(traceCtx, "accepted")

		model := tui.Model(tui.ModelDependencies{
			SessionCtx:   traceCtx,
			User:         *user,
			UserRepo:     deps.UserRepository,
			MatchRepo:    deps.MatchRepository,
			LobbyManager: deps.LobbyManager,
			GameRegistry: deps.GameRegistry,
		})

		if st, ok := lookupSessionState(s); ok {
			st.owns = true
			st.user = user
			st.model = model
		}

		// bubbletea's own recover prints to stderr and knows nothing about the span
		// or the metric, so reportingModel catches Init/Update/View first. Its
		// catching stays enabled all the same: it is the only thing covering the
		// goroutines bubbletea spawns per Cmd, and an unrecovered panic there takes
		// down the process for every connected player, not just this session.
		return reportingModel{Model: model, session: s}, nil
	}
}

// reportingModel reports a panic in the TUI against the session's trace context and
// then quits, so the session ends the same way an idle removal does. It wraps the
// three methods bubbletea calls on the event-loop goroutine; a panic inside a Cmd
// runs on a goroutine bubbletea owns and is left to bubbletea's own recover.
type reportingModel struct {
	tea.Model
	session ssh.Session
}

func (m reportingModel) Init() tea.Cmd {
	defer reportPanic(m.session)
	return m.Model.Init()
}

func (m reportingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// A recovered Update leaves the model on the state the panic interrupted, so
	// the session quits rather than rendering on from a half-applied message.
	defer reportPanic(m.session)
	inner, cmd := m.Model.Update(msg)
	m.Model = inner
	return m, cmd
}

func (m reportingModel) View() tea.View {
	defer reportPanic(m.session)
	return m.Model.View()
}

// reportPanic records a recovered panic and re-panics so the caller's own frame
// unwinds; the panic stops at bubbletea, which ends the program without taking the
// process with it. It must be a direct defer - a recover() one call deeper is nil.
func reportPanic(s ssh.Session) {
	r := recover()
	if r == nil {
		return
	}
	recordSessionPanic(s, r)
	panic(r)
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

// acquireChannelSlot claims one of the connection's session slots. The counter is
// stored on the connection-scoped Context, whose own lock makes the first-writer
// race harmless.
func acquireChannelSlot(s ssh.Session) bool {
	ctx := s.Context()
	ctx.Lock()
	counter, ok := ctx.Value(ctxKeyChannelCount).(*atomic.Int32)
	if !ok {
		counter = new(atomic.Int32)
		ctx.SetValue(ctxKeyChannelCount, counter)
	}
	ctx.Unlock()

	if counter.Add(1) > maxSessionsPerConnection {
		counter.Add(-1)
		return false
	}
	return true
}

func releaseChannelSlot(s ssh.Session) {
	if counter, ok := s.Context().Value(ctxKeyChannelCount).(*atomic.Int32); ok {
		counter.Add(-1)
	}
}

func sessionLifecycle(deps ServerDependencies, tracker *SessionTracker) wish.Middleware {
	return func(sh ssh.Handler) ssh.Handler {
		return func(s ssh.Session) {
			if !acquireChannelSlot(s) {
				observability.SSHSession(s.Context(), "rejected_channel_limit")
				slog.WarnContext(s.Context(), "too many session channels on one connection",
					"remote_addr", s.RemoteAddr().String(), "limit", maxSessionsPerConnection)
				wish.Fatalf(s, "Too many sessions open on this connection.\n")
				return
			}
			defer releaseChannelSlot(s)

			st := startSession(s)
			defer finishSession(s, st)
			defer recoverSession(s)
			defer releaseSession(s, deps, tracker)
			defer closeSessionModel(s)

			sh(s)
		}
	}
}

func startSession(s ssh.Session) *sessionState {
	pty, _, _ := s.Pty()
	ctx, span := otel.Tracer("terminal-card/ssh").Start(s.Context(), "ssh.session",
		trace.WithAttributes(
			attribute.String("remote_addr", s.RemoteAddr().String()),
			attribute.String("client_version", s.Context().ClientVersion()),
			attribute.Int("terminal.width", pty.Window.Width),
			attribute.Int("terminal.height", pty.Window.Height),
		))

	st := &sessionState{traceCtx: ctx, span: span, started: time.Now()}
	sessionStates.Store(s, st)

	slog.InfoContext(ctx, "ssh session connected",
		"remote_addr", s.RemoteAddr().String(),
		"client_version", s.Context().ClientVersion(),
	)
	return st
}

func finishSession(s ssh.Session, st *sessionState) {
	defer sessionStates.Delete(s)

	outcome := "normal"
	if st.panicked {
		outcome = "panic"
	}
	elapsed := time.Since(st.started)

	observability.SSHSessionEnded(st.traceCtx, elapsed, outcome)
	slog.InfoContext(st.traceCtx, "ssh session disconnected",
		"remote_addr", s.RemoteAddr().String(),
		"client_version", s.Context().ClientVersion(),
		"duration_seconds", elapsed.Seconds(),
		"outcome", outcome,
	)

	if st.user != nil {
		st.span.SetAttributes(attribute.String("user", st.user.Username))
	}
	st.span.End()
}

func recoverSession(s ssh.Session) {
	r := recover()
	if r == nil {
		return
	}
	recordSessionPanic(s, r)
	wish.Fatalf(s, "\r\nAn unexpected internal error occurred. The administrators have been notified.\r\n")
}

// recordSessionPanic puts a recovered panic on the session span, the metric and the
// log, all against the session's own trace context. It reports only: whether the
// session can be told about it is the caller's business.
func recordSessionPanic(s ssh.Session, r any) {
	err := fmt.Errorf("panic during ssh session: %v", r)
	ctx := sessionTraceContext(s)
	if st, ok := lookupSessionState(s); ok {
		st.panicked = true
		if st.span != nil {
			st.span.RecordError(err, trace.WithStackTrace(true))
			st.span.SetStatus(codes.Error, "panic during ssh session")
		}
	}
	observability.SSHPanicRecovered(ctx)
	slog.ErrorContext(ctx, "critical panic recovered during ssh session",
		"panic", r,
		"remote_addr", s.RemoteAddr().String(),
	)
}

func closeSessionModel(s ssh.Session) {
	if st, ok := lookupSessionState(s); ok && st.model != nil {
		st.model.Close()
	}
}

// releaseSession gives up the seat before the tracker slot. The other order lets a
// fast reconnect take the slot and then have its lobby seat torn down by the old
// session's LeaveLobby.
func releaseSession(s ssh.Session, deps ServerDependencies, tracker *SessionTracker) {
	st, ok := lookupSessionState(s)
	if !ok || !st.owns || st.user == nil {
		return
	}
	// DisconnectPlayer, not LeaveLobby: a dropped session keeps its mid-game seat
	// for the grace window, so a reconnect resumes the match instead of forfeiting.
	deps.LobbyManager.DisconnectPlayer(lobby.NewPlayer(st.user))
	tracker.Disconnect(st.user.ID)
}
