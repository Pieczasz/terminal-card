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
		panic(err)
	}

	ctx := context.Background()
	otelShutdown, err := observability.SetupOTel(ctx, cfg)
	if err != nil {
		slog.Error("failed to setup OpenTelemetry", "error", err)
		panic(err)
	}
	defer func() {
		if err := otelShutdown(ctx); err != nil {
			slog.Error("failed to shutdown OpenTelemetry", "error", err)
		}
	}()

	// TODO: change logger name
	otelLogger := otelslog.NewLogger("terminal-card-server")
	slog.SetDefault(otelLogger)

	database, err := db.Connect(cfg)
	if err != nil {
		slog.Error("failed to setup database", "error", err)
		panic(err)
	}

	userRepo := repository.NewUserRepository(database)
	matchRepo := repository.NewMatchRepository(database)
	lobbyManager := lobby.NewManager(matchRepo)
	gameRegistry := game.NewRegistry()
	// TODO: dont forget to register games here
	gameRegistry.Register("Crazy Eights", func() game.Rules { return &crazyeight.CrazyEightsRules{} })

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
		panic(err)
	}

	addr := fmt.Sprintf("%s:%d", cfg.ServerHost, cfg.ServerPort)
	lc := net.ListenConfig{}
	listener, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		slog.Error("error creating listener", "error", err)
		panic(err)
	}
	limitListener := netutil.LimitListener(listener, cfg.MaxConnections)

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	slog.Info("starting ssh server", "address", addr, "max_connections", cfg.MaxConnections)
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
