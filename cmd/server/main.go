package main

import (
	"client/internal/ssh"
	"log"
)

func main() {
	server, err := ssh.SetupServer()
	if err != nil {
		log.Fatal(err)
	}

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("server: starting server error: %v", err)
	}
}
