// Package ssh contains implementation for setting up ssh auth, middleware, and
// server setup.
package ssh

import (
	"fmt"
	"path/filepath"

	"client/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/keygen"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/activeterm"
	bm "github.com/charmbracelet/wish/bubbletea"
	lm "github.com/charmbracelet/wish/logging"

	"gorm.io/gorm"
)

func SetupServer(database *gorm.DB) (*ssh.Server, error) {
	key, err := keygen.New(filepath.Join(".wishlist", "server"), keygen.WithKeyType(keygen.Ed25519))
	if err != nil {
		return nil, fmt.Errorf("generating a keygen pair error: %w", err)
	}

	if !key.KeyPairExists() {
		if err := key.WriteKeys(); err != nil {
			return nil, fmt.Errorf("error while saving keypair to disk: %w", err)
		}
	}

	server, err := wish.NewServer(
		wish.WithAddress("0.0.0.0:6969"),
		wish.WithHostKeyPEM(key.RawPrivateKey()),

		wish.WithPublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
			return true
		}),

		wish.WithMiddleware(
			bm.Middleware(func(s ssh.Session) (tea.Model, []tea.ProgramOption) {
				user, err := AuthenticateAndLoadUser(database, s)
				if err != nil {
					wish.Fatalf(s, "%v\n", err)
					return nil, nil
				}

				return tui.AppModel(*user, database), []tea.ProgramOption{
					tea.WithAltScreen(),
				}
			}),
			lm.Middleware(),
			activeterm.Middleware(),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("error while setting up wish ssh server: %w", err)
	}

	return server, nil
}
