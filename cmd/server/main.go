package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Pieczasz/terminal-card/internal/catalog"
	"github.com/Pieczasz/terminal-card/internal/config"
	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/httpapi"
	"github.com/Pieczasz/terminal-card/internal/lobby"
	"github.com/Pieczasz/terminal-card/internal/observability"
	"github.com/Pieczasz/terminal-card/internal/repository"
	"github.com/Pieczasz/terminal-card/internal/ssh"

	charmssh "charm.land/ssh"
	"github.com/pires/go-proxyproto"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"golang.org/x/net/netutil"
)

// Shutdown spends these budgets sequentially, in this order: the ssh drain, the
// stats api, the match finalizers, then the OTel flush. Worst case that is
// 30+5+15+5 = 55s, plus a second finalizer window of the same 15s if the first one
// lapses. compose's stop_grace_period must exceed the total or the runtime kills the
// process in the middle of a match write.
const (
	sshDrainTimeout      = 30 * time.Second
	apiDrainTimeout      = 5 * time.Second
	finalizeDrainTimeout = 15 * time.Second
	otelDrainTimeout     = 5 * time.Second
)

func main() {
	// -healthcheck probes the local stats API and exits: the container image is
	// distroless, so the server binary doubles as the compose healthcheck command.
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		os.Exit(healthcheck())
	}
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func healthcheck() int {
	port := cmp.Or(os.Getenv("API_PORT"), "6970")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+port+"/healthz", nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck: status", resp.StatusCode)
		return 1
	}
	return 0
}

func run() (err error) {
	// First statement in the process: a config or OTel failure below has to reach the
	// JSON handler rather than the default text one. The level starts at info and is
	// raised or lowered once the config is readable.
	logLevel := installLogging()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	logLevel.Set(cfg.LogLevel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// This defer chain is LIFO and load-bearing; do not reorder it.
	otelCleanup, err := setupOTel(ctx, cfg)
	if err != nil {
		return fmt.Errorf("setup OpenTelemetry: %w", err)
	}
	defer otelCleanup()
	defer func() {
		if err != nil {
			slog.ErrorContext(ctx, "server exited with error", "error", err)
		}
	}()

	database, err := db.Connect(cfg)
	if err != nil {
		return fmt.Errorf("setup database: %w", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		return fmt.Errorf("get sql.DB: %w", err)
	}
	defer func() {
		if err := sqlDB.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close database", "error", err)
		}
	}()
	// The pool is a hard cap that queues silently, so these gauges are the only
	// warning before saturation. Losing them is not worth failing a boot over.
	if err := observability.RegisterDBStats(sqlDB); err != nil {
		slog.ErrorContext(ctx, "failed to register database pool metrics", "error", err)
	}

	userRepo := repository.NewUserRepository(database)
	matchRepo := repository.NewMatchRepository(database)
	lobbyManager := lobby.NewManager(ctx, matchRepo)

	// Registered after the sqlDB.Close defer above, so LIFO stops new match writes
	// and drains registered ones before the handle they write through is closed.
	defer waitForFinalizers(lobbyManager)

	server, tracker, err := newSSHServer(cfg, userRepo, matchRepo, lobbyManager)
	if err != nil {
		return err
	}

	stopAPI, apiErr := startStatsAPI(cfg, tracker, lobbyManager, userRepo, sqlDB.PingContext)
	defer stopAPI()

	return serve(ctx, serveDeps{
		config:     cfg,
		sshServer:  server,
		apiErr:     apiErr,
		onShutdown: lobbyManager.BeginShutdown,
	})
}

// installLogging fans slog output to stderr and to the OTel logger provider, so a
// line is both visible to the container runtime and exported. MultiHandler clones
// the record per sink and joins their errors, so one broken sink never hides another.
// The returned LevelVar is shared by both sinks and settable once config is loaded.
func installLogging() *slog.LevelVar {
	level := new(slog.LevelVar)
	level.Set(slog.LevelInfo)
	slog.SetDefault(slog.New(slog.NewMultiHandler(
		slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}),
		levelGate{Handler: otelslog.NewHandler("terminal-card"), level: level},
	)))
	return level
}

// levelGate applies a level to a handler that has none of its own. The otelslog
// bridge exports whatever it is handed, so without this LOG_LEVEL would only quiet
// stderr while still shipping every debug line to the collector.
type levelGate struct {
	slog.Handler
	level slog.Leveler
}

func (g levelGate) Enabled(_ context.Context, l slog.Level) bool { return l >= g.level.Level() }

func (g levelGate) WithAttrs(attrs []slog.Attr) slog.Handler {
	return levelGate{Handler: g.Handler.WithAttrs(attrs), level: g.level}
}

func (g levelGate) WithGroup(name string) slog.Handler {
	return levelGate{Handler: g.Handler.WithGroup(name), level: g.level}
}

// setupOTel pairs export setup with the cleanup to defer, so the shutdown budget
// cannot drift away from the one the drain comment accounts for.
func setupOTel(ctx context.Context, cfg *config.Config) (func(), error) {
	shutdown, err := observability.SetupOTel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("setup otel: %w", err)
	}
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), otelDrainTimeout)
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			slog.ErrorContext(shutdownCtx, "failed to shutdown OpenTelemetry", "error", err)
		}
	}, nil
}

// newSSHServer wires the game registry and repositories into the ssh server. The
// tracker comes back because the stats api shares it to count who is online.
func newSSHServer(
	cfg *config.Config,
	userRepo db.UserRepository,
	matchRepo db.MatchRepository,
	lobbyManager *lobby.Manager,
) (*charmssh.Server, *ssh.SessionTracker, error) {
	// MaxConnections is the player-visible session cap: the tracker refuses the
	// overflow with a message, while the TCP LimitListener below only backstops
	// handshake floods at twice that, so a full server says so instead of hanging.
	tracker := ssh.NewSessionTracker(cfg.MaxConnections)
	server, err := ssh.SetupServer(ssh.ServerDependencies{
		Config:          cfg,
		UserRepository:  userRepo,
		MatchRepository: matchRepo,
		LobbyManager:    lobbyManager,
		GameRegistry:    buildRegistry(),
		Tracker:         tracker,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("setup ssh server: %w", err)
	}
	return server, tracker, nil
}

// buildRegistry turns the catalog into the registry. catalog.All is the only place a
// game is declared, so this cannot drift from what the TUI routes to.
func buildRegistry() *game.Registry {
	registry := game.NewRegistry()
	for _, e := range catalog.All {
		registry.RegisterModule(e.Module())
	}
	return registry
}

// startStatsAPI returns the shutdown hook and a channel carrying a serve failure. A
// bind error used to be a log line nobody reads and a website whose numbers silently
// stopped moving, so it reaches run's error path instead.
func startStatsAPI(
	cfg *config.Config,
	tracker *ssh.SessionTracker,
	lobbyManager *lobby.Manager,
	userRepo db.UserRepository,
	health func(ctx context.Context) error,
) (func(), <-chan error) {
	addr := fmt.Sprintf("%s:%d", cfg.ServerHost, cfg.APIPort)
	srv := httpapi.Serve(addr, httpapi.Handler(httpapi.Deps{
		Sessions:          tracker,
		Lobbies:           lobbyManager,
		Users:             userRepo,
		AllowOrigin:       cfg.APIAllowOrigin,
		RequestsPerMinute: cfg.APIRequestsPerMinute,
		TrustedProxy:      cfg.APITrustProxy,
		Health:            health,
	}))

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("starting stats api", "address", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- fmt.Errorf("stats api stopped: %w", err)
		}
	}()

	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), apiDrainTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Warn("stats api shutdown was not clean", "error", err)
		}
	}, serveErr
}

func waitForFinalizers(lobbyManager *lobby.Manager) {
	if lobbyManager.WaitForFinalizers(finalizeDrainTimeout) {
		return
	}
	slog.Warn("match finalizers exceeded their deadline; giving them one more window",
		"timeout", finalizeDrainTimeout)
	// Capped rather than unbounded: a wedged write must not hold the process open
	// past the runtime's kill timer, which would lose the log line explaining why.
	if !lobbyManager.WaitForFinalizers(finalizeDrainTimeout) {
		slog.Error("abandoning match finalizers; a finished match may be missing from history",
			"timeout", finalizeDrainTimeout)
	}
}

type sshServer interface {
	Serve(net.Listener) error
	Shutdown(context.Context) error
	Close() error
}

type serveDeps struct {
	config    *config.Config
	sshServer sshServer
	apiErr    <-chan error
	// onShutdown runs the moment a signal arrives, before any session is drained: a
	// match ending because of the deploy must not be rated.
	onShutdown func()
}

func serve(ctx context.Context, d serveDeps) error {
	cfg, server := d.config, d.sshServer
	addr := fmt.Sprintf("%s:%d", cfg.ServerHost, cfg.ServerPort)
	lc := net.ListenConfig{}
	listener, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("create tcp listener: %w", err)
	}
	limitListener := netutil.LimitListener(listener, 2*cfg.MaxConnections)
	// The default proxyproto policy is REQUIRE: every connection must open with a
	// PROXY header, which is right behind nginx (and is why 6969 must never be
	// published - any peer's header is honored). PROXY_PROTOCOL=false is the local
	// development escape hatch for bare ssh clients.
	acceptListener := limitListener
	if cfg.ProxyProtocol {
		acceptListener = &proxyproto.Listener{Listener: limitListener, ReadHeaderTimeout: 10 * time.Second}
	}
	defer func() {
		if err := acceptListener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			slog.WarnContext(ctx, "failed to close listener", "error", err)
		}
	}()

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(done)

	slog.InfoContext(ctx, "starting ssh server",
		"address", addr,
		"max_connections", cfg.MaxConnections,
		"version", cfg.ServiceVersion,
	)
	serveErr := make(chan error, 1)
	go func() {
		err := server.Serve(acceptListener)
		if errors.Is(err, charmssh.ErrServerClosed) {
			// Expected: our own Shutdown below unblocks Serve this way.
			err = nil
		}
		serveErr <- err
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("ssh accept loop failed: %w", err)
		}
		return errors.New("ssh server stopped accepting connections unexpectedly")
	case err := <-d.apiErr:
		stopServer(server)
		return err
	case <-done:
	}

	if d.onShutdown != nil {
		d.onShutdown()
	}
	stopServer(server)
	return nil
}

// stopServer drains in-flight sessions, then closes whatever is left.
//
// A table still playing when the deploy lands is the normal case, not a failed
// shutdown: treating the drain deadline as an error reported a healthy redeploy as a
// failure and, worse, returned without ever closing the server, so every session
// stayed open until the runtime killed the process.
func stopServer(server sshServer) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), sshDrainTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.WarnContext(shutdownCtx, "sessions outlasted the drain window; closing them",
			"error", err, "timeout", sshDrainTimeout)
	}
	// Shutdown waits, Close is what lets go, so it runs on both paths.
	if err := server.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		slog.Warn("failed to close ssh server", "error", err)
	}
}
