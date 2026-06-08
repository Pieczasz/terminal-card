// Package ssh contains implementation for setting up ssh auth, middleware, and
// server setup.
package ssh

import (
	"fmt"
	"sync"

	"terminalcard/internal/config"
	"terminalcard/internal/db"
	"terminalcard/internal/game"
	"terminalcard/internal/lobby"
	"terminalcard/internal/player"
	"terminalcard/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/keygen"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/activeterm"
	bm "github.com/charmbracelet/wish/bubbletea"
	lm "github.com/charmbracelet/wish/logging"
)

// TODO: add more tracking? GDPR? legal analytics?
type SessionTracker struct {
	mu     sync.Mutex
	active map[uint]bool
}

func NewSessionTracker() *SessionTracker {
	return &SessionTracker{
		active: make(map[uint]bool),
	}
}

func (t *SessionTracker) Connect(userID uint) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.active[userID] {
		return false
	}
	t.active[userID] = true
	return true
}

func (t *SessionTracker) Disconnect(userID uint) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.active, userID)
}

type ServerDependencies struct {
	Config       *config.Config
	Queries      *db.Queries
	LobbyManager *lobby.Manager
	GameRegistry *game.Registry
}

func SetupServer(deps ServerDependencies) (*ssh.Server, error) {
	key, err := keygen.New(deps.Config.SSHKeyPath, keygen.WithKeyType(keygen.Ed25519))
	if err != nil {
		return nil, fmt.Errorf("generating a keygen pair error: %w", err)
	}

	if !key.KeyPairExists() {
		if err := key.WriteKeys(); err != nil {
			return nil, fmt.Errorf("error while saving keypair to disk: %w", err)
		}
	}

	tracker := NewSessionTracker()

	server, err := wish.NewServer(
		// TODO: refactor to normal address instead of localhost
		wish.WithAddress(fmt.Sprintf("0.0.0.0:%d", deps.Config.ServerPort)),
		wish.WithHostKeyPEM(key.RawPrivateKey()),
		wish.WithPublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
			return true
		}),
		wish.WithMiddleware(
			func(sh ssh.Handler) ssh.Handler {
				return func(s ssh.Session) {
					sh(s)
					user, err := AuthenticateAndLoadUser(deps.Queries, s)
					if err == nil && s.Context().Value("owns_connection") == true {
						tracker.Disconnect(user.ID)
						p := &player.Player{Id: fmt.Sprint(user.ID), DatabaseUser: user}
						deps.LobbyManager.LeaveLobby(p)
					}
				}
			},

			bm.Middleware(func(s ssh.Session) (tea.Model, []tea.ProgramOption) {
				// TODO: refactor getting user multiple times?
				user, err := AuthenticateAndLoadUser(deps.Queries, s)
				if err != nil {
					wish.Fatalf(s, "%v\n", err)
					return nil, nil
				}

				if !tracker.Connect(user.ID) {
					wish.Fatalf(s, "Account '%s' is already connected from another session.\n", user.Username)
					return nil, nil
				}

				s.Context().SetValue("owns_connection", true)

				return tui.Model(*user, deps.Queries, deps.LobbyManager, deps.GameRegistry), []tea.ProgramOption{
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
