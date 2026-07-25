package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Pieczasz/terminal-card/internal/config"
	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/game/crazyeight"
	"github.com/Pieczasz/terminal-card/internal/game/poker"
	"github.com/Pieczasz/terminal-card/internal/lobby"
	"github.com/Pieczasz/terminal-card/internal/observability"
	"github.com/Pieczasz/terminal-card/internal/repository"
	"github.com/Pieczasz/terminal-card/internal/ssh"

	"github.com/pires/go-proxyproto"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"golang.org/x/net/netutil"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	otelShutdown, err := observability.SetupOTel(ctx, cfg)
	if err != nil {
		slog.Error("failed to setup OpenTelemetry", "error", err)
		os.Exit(1)
	}
	defer func() {
		shutdownOTelCtx, otelCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer otelCancel()
		if err := otelShutdown(shutdownOTelCtx); err != nil {
			slog.Error("failed to shutdown OpenTelemetry", "error", err)
		}
	}()

	slog.SetDefault(slog.New(observability.NewFanoutHandler(
		slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}),
		otelslog.NewHandler("terminal-card"),
	)))

	if err := db.Migrate(cfg); err != nil {
		slog.Error("failed to run database migrations", "error", err)
		os.Exit(1)
	}

	database, err := db.Connect(cfg)
	if err != nil {
		slog.Error("failed to setup database", "error", err)
		os.Exit(1)
	}
	sqlDB, err := database.DB()
	if err != nil {
		slog.Error("failed to get sql.DB", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := sqlDB.Close(); err != nil {
			slog.Error("failed to close database", "error", err)
		}
	}()

	userRepo := repository.NewUserRepository(database)
	matchRepo := repository.NewMatchRepository(database)
	lobbyManager := lobby.NewManagerWithContext(ctx, matchRepo)
	gameRegistry := game.NewRegistry()
	gameRegistry.RegisterModule(game.Module{
		Name:    "Crazy Eights",
		Slug:    "crazy_eights",
		Factory: func() game.Rules { return &crazyeight.Rules{} },
	})
	gameRegistry.RegisterModule(game.Module{
		Name:    "Poker",
		Slug:    "poker",
		Factory: func() game.Rules { return &poker.Rules{} },
	})

	deps := ssh.ServerDependencies{
		Config:          cfg,
		UserRepository:  userRepo,
		MatchRepository: matchRepo,
		LobbyManager:    lobbyManager,
		GameRegistry:    gameRegistry,
	}
	server, err := ssh.SetupServer(deps)
	if err != nil {
		slog.Error("error while setting up the server", "error", err)
		os.Exit(1)
	}

	addr := fmt.Sprintf("%s:%d", cfg.ServerHost, cfg.ServerPort)
	lc := net.ListenConfig{}
	listener, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		slog.Error("error creating listener", "error", err)
		os.Exit(1)
	}
	limitListener := netutil.LimitListener(listener, cfg.MaxConnections)

	// nginx prepends a PROXY protocol header so RemoteAddr is the real client
	// IP (per-IP rate limiting, logs, traces) instead of the proxy's.
	// ponytail: default policy trusts the header on any connection, which is
	// safe only because the backend port is reachable solely via the trusted
	// proxy; add a Policy if the port is ever exposed directly.
	proxyListener := &proxyproto.Listener{Listener: limitListener, ReadHeaderTimeout: 10 * time.Second}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	slog.Info("starting ssh server",
		"address", addr,
		"max_connections", cfg.MaxConnections,
		"version", cfg.ServiceVersion,
	)
	go func() {
		if err := server.Serve(proxyListener); err != nil {
			slog.Error("server: starting ssh server error", "error", err)
		}
	}()

	<-done
	slog.Info("stopping server")
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("failed to stop server gracefully", "error", err)
	}
}
