package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"terminalcard/internal/config"
	"terminalcard/internal/db"
	"terminalcard/internal/game"
	"terminalcard/internal/game/crazyeight"
	"terminalcard/internal/lobby"
	"terminalcard/internal/observability"
	"terminalcard/internal/repository"
	"terminalcard/internal/ssh"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"golang.org/x/net/netutil"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()
	otelShutdown, err := observability.SetupOTel(ctx, cfg)
	if err != nil {
		slog.Error("failed to setup OpenTelemetry", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := otelShutdown(ctx); err != nil {
			slog.Error("failed to shutdown OpenTelemetry", "error", err)
		}
	}()

	otelLogger := otelslog.NewLogger("terminal-card")
	slog.SetDefault(otelLogger)

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
		Factory: func() game.Rules { return &crazyeight.CrazyEightsRules{} },
	})
	// Poker is WIP and intentionally not registered for v0.1.

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

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	slog.Info("starting ssh server",
		"address", addr,
		"max_connections", cfg.MaxConnections,
		"version", cfg.ServiceVersion,
	)
	go func() {
		if err := server.Serve(limitListener); err != nil {
			slog.Error("server: starting ssh server error", "error", err)
		}
	}()

	<-done
	slog.Info("stopping server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("failed to stop server gracefully", "error", err)
	}
}
