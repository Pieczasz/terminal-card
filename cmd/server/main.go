package main

import (
	"client/internal/db"
	"client/internal/game"
	"client/internal/game/crazyeight"
	"client/internal/lobby"
	"client/internal/ssh"
	"log/slog"
)

func main() {
	database, err := db.Connect()
	if err != nil {
		slog.Error("failed to setup database", "error", err)
		panic(err)
	}

	lobbyManager := lobby.NewManager()
	gameRegistry := game.NewRegistry()
	gameRegistry.Register("Crazy Eights", func() game.Rules { return &crazyeight.CrazyEightsRules{} })

	server, err := ssh.SetupServer(database, lobbyManager, gameRegistry)
	if err != nil {
		slog.Error("error while setting up the server", "error", err)
		panic(err)
	}

	if err := server.ListenAndServe(); err != nil {
		slog.Error("server: starting ssh server error", "error", err)
		panic(err)
	}

}
