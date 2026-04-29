package main

import (
	"client/internal/db"
	"client/internal/lobby"
	"client/internal/ssh"
	"log/slog"
)

func init() {
	lobbyManager := lobby.NewManager()
}

func main() {
	database, err := db.Connect()
	if err != nil {
		slog.Error("failed to setup database", "error", err)
		panic(err)
	}

	server, err := ssh.SetupServer(database)
	if err != nil {
		slog.Error("error while setting up the server", "error", err)
		panic(err)
	}

	if err := server.ListenAndServe(); err != nil {
		slog.Error("server: starting ssh server error", "error", err)
		panic(err)
	}

}
