package main

import (
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

// finalizeDrainTimeout matches the per-finalizer deadline.
const finalizeDrainTimeout = 15 * time.Second

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() (err error) {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		return fmt.Errorf("load configuration: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	otelShutdown, err := observability.SetupOTel(ctx, cfg)
	if err != nil {
		slog.Error("failed to setup OpenTelemetry", "error", err)
		return fmt.Errorf("setup OpenTelemetry: %w", err)
	}
	defer func() {
		shutdownOTelCtx, otelCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer otelCancel()
		if err := otelShutdown(shutdownOTelCtx); err != nil {
			slog.Error("failed to shutdown OpenTelemetry", "error", err)
		}
	}()

	// Report the exit cause here rather than in main: this defer is registered
	// after the OTel shutdown above, so LIFO runs it first and the record still
	// reaches the logger provider instead of a torn-down one.
	defer func() {
		if err != nil {
			slog.Error("server exited with error", "error", err)
		}
	}()

	installLogging()

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
			slog.Error("failed to close database", "error", err)
		}
	}()

	userRepo := repository.NewUserRepository(database)
	matchRepo := repository.NewMatchRepository(database)
	lobbyManager := lobby.NewManager(ctx, matchRepo)

	// Registered after the sqlDB.Close defer above, so LIFO stops new match writes
	// and drains registered ones before the handle they write through is closed.
	defer waitForFinalizers(lobbyManager)

	gameRegistry := buildRegistry()

	// Shared so the stats endpoint can read the live session count.
	tracker := ssh.NewSessionTracker()

	deps := ssh.ServerDependencies{
		Config:          cfg,
		UserRepository:  userRepo,
		MatchRepository: matchRepo,
		LobbyManager:    lobbyManager,
		GameRegistry:    gameRegistry,
		Tracker:         tracker,
	}
	server, err := ssh.SetupServer(deps)
	if err != nil {
		return fmt.Errorf("setup ssh server: %w", err)
	}

	stopAPI := startStatsAPI(cfg, tracker, lobbyManager, userRepo)
	defer stopAPI()

	return serve(ctx, cfg, server)
}

// installLogging fans slog output to stderr and to the OTel logger provider, so a
// line is both visible to the container runtime and exported.
func installLogging() {
	slog.SetDefault(slog.New(observability.NewFanoutHandler(
		slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}),
		otelslog.NewHandler("terminal-card"),
	)))
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

// startStatsAPI runs the read-only JSON feed the website reads. It is intentionally
// best-effort: the game is the product, so a failure to bind the stats port logs and
// carries on rather than refusing to start the server.
func startStatsAPI(
	cfg *config.Config,
	tracker *ssh.SessionTracker,
	lobbyManager *lobby.Manager,
	userRepo db.UserRepository,
) func() {
	addr := fmt.Sprintf("%s:%d", cfg.ServerHost, cfg.APIPort)
	srv := httpapi.Serve(addr, httpapi.Handler(httpapi.Deps{
		Sessions:     tracker,
		Lobbies:      lobbyManager,
		Users:        userRepo,
		AllowOrigin:  cfg.APIAllowOrigin,
		TrustedProxy: cfg.APITrustProxy,
	}))

	go func() {
		slog.Info("starting stats api", "address", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("stats api stopped", "error", err)
		}
	}()

	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Warn("stats api shutdown was not clean", "error", err)
		}
	}
}

func waitForFinalizers(lobbyManager *lobby.Manager) {
	if !lobbyManager.WaitForFinalizers(finalizeDrainTimeout) {
		slog.Warn("match finalizers exceeded their deadline; waiting for exit",
			"timeout", finalizeDrainTimeout)
		lobbyManager.WaitForFinalizers(0)
	}
}

// sshServer is the part of the wish server that serve drives. It exists as a test
// seam: the accept-loop failure paths below cannot be exercised through a real
// *charmssh.Server, whose Serve blocks on a live listener. That is the only
// production implementation.
type sshServer interface {
	Serve(net.Listener) error
	Shutdown(context.Context) error
}

// serve accepts connections until a shutdown signal arrives, then drains in-flight
// sessions. Returning unwinds run's deferred cleanup, so telemetry still flushes.
func serve(ctx context.Context, cfg *config.Config, server sshServer) error {
	addr := fmt.Sprintf("%s:%d", cfg.ServerHost, cfg.ServerPort)
	lc := net.ListenConfig{}
	listener, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("create tcp listener: %w", err)
	}
	limitListener := netutil.LimitListener(listener, cfg.MaxConnections)
	proxyListener := &proxyproto.Listener{Listener: limitListener, ReadHeaderTimeout: 10 * time.Second}
	defer func() {
		// Serve closes the listener on its own way out, so a second close is
		// expected and only the unexpected kind is worth reporting.
		if err := proxyListener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			slog.Warn("failed to close listener", "error", err)
		}
	}()

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)
	// Without Stop the handler outlives serve. That is invisible in production (the
	// process is exiting) but in a test binary it swallows Ctrl-C for good.
	defer signal.Stop(done)

	slog.Info("starting ssh server",
		"address", addr,
		"max_connections", cfg.MaxConnections,
		"version", cfg.ServiceVersion,
	)
	serveErr := make(chan error, 1)
	go func() {
		err := server.Serve(proxyListener)
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
	case <-done:
	}

	slog.Info("stopping server")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("stop server gracefully: %w", err)
	}
	return nil
}
