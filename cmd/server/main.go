package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Pieczasz/terminal-card/internal/catalog"
	"github.com/Pieczasz/terminal-card/internal/config"
	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/lobby"
	"github.com/Pieczasz/terminal-card/internal/observability"
	"github.com/Pieczasz/terminal-card/internal/repository"
	"github.com/Pieczasz/terminal-card/internal/ssh"

	charmssh "github.com/charmbracelet/ssh"
	"github.com/pires/go-proxyproto"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"golang.org/x/net/netutil"
)

// finalizeDrainTimeout bounds how long shutdown waits for in-flight ranked match
// writes before closing the database handle.
const finalizeDrainTimeout = 10 * time.Second

func main() {
	if err := run(); err != nil {
		// run reports the cause itself, while the telemetry pipeline is still up.
		os.Exit(1)
	}
}

// run holds the whole server lifecycle so that every failure path unwinds through
// the deferred cleanups. Calling os.Exit here would skip them and drop buffered
// telemetry along with the database handle.
func run() (err error) {
	cfg, err := config.Load()
	if err != nil {
		// Before OTel exists there is nowhere to report this but stderr.
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

	slog.SetDefault(slog.New(observability.NewFanoutHandler(
		slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}),
		otelslog.NewHandler("terminal-card"),
	)))

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
	lobbyManager := lobby.NewManagerWithContext(ctx, matchRepo)

	// Registered after the sqlDB.Close defer above, so LIFO drains in-flight ranked
	// finalizes before the handle they write through is closed. The app context is
	// cancelled later still (run's first defer), so a finalize in progress keeps its
	// own deadline rather than being cut off here.
	defer func() {
		if !lobbyManager.WaitForFinalizers(finalizeDrainTimeout) {
			slog.Warn("timed out draining ranked match finalizers", "timeout", finalizeDrainTimeout)
		}
	}()

	gameRegistry := game.NewRegistry()
	for _, e := range catalog.All {
		gameRegistry.RegisterModule(e.Module())
	}

	deps := ssh.ServerDependencies{
		Config:          cfg,
		UserRepository:  userRepo,
		MatchRepository: matchRepo,
		LobbyManager:    lobbyManager,
		GameRegistry:    gameRegistry,
	}
	server, err := ssh.SetupServer(deps)
	if err != nil {
		return fmt.Errorf("setup ssh server: %w", err)
	}

	return serve(ctx, cfg, server)
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

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	slog.Info("starting ssh server",
		"address", addr,
		"max_connections", cfg.MaxConnections,
		"version", cfg.ServiceVersion,
	)
	// Buffered so the goroutine never blocks once we stop reading after a signal.
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
		// The accept loop ended before any shutdown signal, so the process must not
		// keep running with a listener nobody serves - it would pass a TCP health
		// check while every login hangs.
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
