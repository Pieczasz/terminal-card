// Package ssh contains implementation for setting up ssh auth, middleware, and
// server setup.
package ssh

import (
	"fmt"

	"client/internal/config"
	"client/internal/db"
	"client/internal/game"
	"client/internal/lobby"
	"client/internal/player"
	"client/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/keygen"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/activeterm"
	bm "github.com/charmbracelet/wish/bubbletea"
	lm "github.com/charmbracelet/wish/logging"
)

func SetupServer(cfg *config.Config, queries *db.Queries, lobbyManager *lobby.Manager, gameRegistry *game.Registry) (*ssh.Server, error) {
	key, err := keygen.New(cfg.SSHKeyPath, keygen.WithKeyType(keygen.Ed25519))
	if err != nil {
		return nil, fmt.Errorf("generating a keygen pair error: %w", err)
	}

	if !key.KeyPairExists() {
		if err := key.WriteKeys(); err != nil {
			return nil, fmt.Errorf("error while saving keypair to disk: %w", err)
		}
	}

	server, err := wish.NewServer(
		wish.WithAddress(fmt.Sprintf("0.0.0.0:%d", cfg.ServerPort)),
		wish.WithHostKeyPEM(key.RawPrivateKey()),

		wish.WithPublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
			return true
		}),

		wish.WithMiddleware(
			func(sh ssh.Handler) ssh.Handler {
				return func(s ssh.Session) {
					sh(s)

					user, err := AuthenticateAndLoadUser(queries, s)
					if err == nil {
						p := &player.Player{Id: fmt.Sprint(user.ID), DatabaseUser: user}
						lobbyManager.LeaveLobby(p)
					}
				}
			},
			bm.Middleware(func(s ssh.Session) (tea.Model, []tea.ProgramOption) {
				user, err := AuthenticateAndLoadUser(queries, s)
				if err != nil {
					wish.Fatalf(s, "%v\n", err)
					return nil, nil
				}

				return tui.Model(*user, queries, lobbyManager, gameRegistry), []tea.ProgramOption{
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
