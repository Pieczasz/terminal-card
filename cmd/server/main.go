package main

import (
	"client/internal/db"
	"client/internal/game"
	"client/internal/game/crazyeight"
	"client/internal/lobby"
	"client/internal/ssh"
	"log/slog"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func main() {
	lipgloss.SetColorProfile(termenv.TrueColor)
	database, err := db.Connect()
	if err != nil {
		slog.Error("failed to setup database", "error", err)
		panic(err)
	}

	lobbyManager := lobby.NewManager()
	gameRegistry := game.NewRegistry()
	gameRegistry.Register("Crazy Eights", func() game.Rules { return &crazyeight.CrazyEightsRules{} })

	queries := db.NewQueries(database)
	server, err := ssh.SetupServer(queries, lobbyManager, gameRegistry)
	if err != nil {
		slog.Error("error while setting up the server", "error", err)
		panic(err)
	}

	if err := server.ListenAndServe(); err != nil {
		slog.Error("server: starting ssh server error", "error", err)
		panic(err)
	}

}
