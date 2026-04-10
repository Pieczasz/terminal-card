// Package ssh contains implementation for setting up ssh auth, middleware and
// server setup.
package ssh

import (
	"client/internal/tui"
	"fmt"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/keygen"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/activeterm"
	bm "github.com/charmbracelet/wish/bubbletea"
	lm "github.com/charmbracelet/wish/logging"
)

func SetupServer() (*ssh.Server, error) {
	key, err := keygen.New(filepath.Join(".wishlist", "server"), keygen.WithKeyType(keygen.Ed25519))
	if err != nil {
		return nil, fmt.Errorf("server: generating a keygen pair error: %v ", err)
	}

	if !key.KeyPairExists() {
		if err := key.WriteKeys(); err != nil {
			return nil, fmt.Errorf("server: error while saving keypair to disk: %v", err)
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
				return tui.AppModel(), []tea.ProgramOption{
					tea.WithAltScreen(),
				}
			}),
			lm.Middleware(),
			activeterm.Middleware(),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("server: error while setting up wish ssh server: %v ", err)
	}

	return server, err
}
