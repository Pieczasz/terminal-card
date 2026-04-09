package main

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/keygen"
	"github.com/charmbracelet/log"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/activeterm"
	bm "github.com/charmbracelet/wish/bubbletea"
	lm "github.com/charmbracelet/wish/logging"
)

func main() {
	key, err := keygen.New(filepath.Join(".wishlist", "server"), keygen.WithKeyType(keygen.Ed25519))
	if err != nil {
		log.Fatalf("server: generating a keygen pair error: %v ", err)
	}

	if !key.KeyPairExists() {
		if err := key.WriteKeys(); err != nil {
			log.Fatalf("server: error while saving keypair to disk: %v", err)
		}
	}

	server, err := wish.NewServer(
		wish.WithAddress("localhost:6969"),
		wish.WithHostKeyPEM(key.RawPrivateKey()),
		// wish.WithPublicKeyAuth(func(ctx ssh.Session, key ssh.PublicKey) bool {
		// 	// Allow anyone to enter the server
		// 	return true
		// }),
		wish.WithMiddleware(
			bm.Middleware(func(s ssh.Session) (tea.Model, []tea.ProgramOption) {
				return AppModel(), []tea.ProgramOption{
					tea.WithAltScreen(),
				}
			}),
		),
		lm.Middleware(),
		activeterm.Middleware(),
	)
	if err != nil {
		log.Fatalf("server: error while setting up wish ssh server: %v ", err)
	}

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("server: starting server error: %v", err)
	}
}
