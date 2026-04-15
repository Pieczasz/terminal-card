package main

import (
	"client/internal/ssh"
	"log"
)

func main() {
	server, err := ssh.SetupServer()
	if err != nil {
		log.Fatalf("error while setting up the server: %v ", err)
	}

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("server: starting ssh server error: %v", err)
	}
}
