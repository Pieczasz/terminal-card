package main

import (
	"context"
	"log/slog"
	"terminalcard/internal/config"
	"terminalcard/internal/db"
	"terminalcard/internal/game"
	"terminalcard/internal/game/crazyeight"
	"terminalcard/internal/lobby"
	"terminalcard/internal/observability"
	"terminalcard/internal/ssh"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"go.opentelemetry.io/contrib/bridges/otelslog"
)

func main() {
	lipgloss.SetColorProfile(termenv.TrueColor)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		panic(err)
	}

	ctx := context.Background()
	otelShutdown, err := observability.SetupOTel(ctx, cfg)
	if err != nil {
		slog.Error("failed to setup OpenTelemetry", "error", err)
	} else {
		defer func() {
			if err := otelShutdown(ctx); err != nil {
				slog.Error("failed to shutdown OpenTelemetry", "error", err)
			}
		}()

		otelLogger := otelslog.NewLogger("terminal-card-server")
		slog.SetDefault(otelLogger)
	}

	database, err := db.Connect(cfg)
	if err != nil {
		slog.Error("failed to setup database", "error", err)
		panic(err)
	}

	lobbyManager := lobby.NewManager()
	gameRegistry := game.NewRegistry()
	gameRegistry.Register("Crazy Eights", func() game.Rules { return &crazyeight.CrazyEightsRules{} })

	queries := db.NewQueries(database)
	deps := ssh.ServerDependencies{
		Config:       cfg,
		Queries:      queries,
		LobbyManager: lobbyManager,
		GameRegistry: gameRegistry,
	}
	server, err := ssh.SetupServer(deps)
	if err != nil {
		slog.Error("error while setting up the server", "error", err)
		panic(err)
	}

	if err := server.ListenAndServe(); err != nil {
		slog.Error("server: starting ssh server error", "error", err)
		panic(err)
	}
}
