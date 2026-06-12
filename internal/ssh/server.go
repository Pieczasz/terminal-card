// Package ssh contains implementation for setting up ssh auth, middleware, and
// server setup.
package ssh

import (
	"fmt"
	"log/slog"
	"net"
	"sync"
	"terminalcard/internal/config"
	"terminalcard/internal/db"
	"terminalcard/internal/game"
	"terminalcard/internal/lobby"
	"terminalcard/internal/player"
	"terminalcard/internal/ratelimit"
	"terminalcard/internal/tui"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/keygen"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/activeterm"
	bm "github.com/charmbracelet/wish/bubbletea"
	lm "github.com/charmbracelet/wish/logging"
)

func RateLimitMiddleware(limiter *ratelimit.SlidingWindowLimiter) wish.Middleware {
	return func(sh ssh.Handler) ssh.Handler {
		return func(s ssh.Session) {
			host, _, err := net.SplitHostPort(s.RemoteAddr().String())
			if err != nil {
				host = s.RemoteAddr().String()
			}
			if !limiter.Allow(host) {
				wish.Fatalf(s, "Too many connections. Please try again later.\n")
				return
			}
			sh(s)
		}
	}
}

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
	Config          *config.Config
	UserRepository  db.UserRepository
	MatchRepository db.MatchRepository
	LobbyManager    *lobby.Manager
	GameRegistry    *game.Registry
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
	// Allow 5 connections per 1 second sliding window
	rateLimiter := ratelimit.NewSlidingWindowLimiter(5, time.Second)

	server, err := wish.NewServer(
		wish.WithAddress(fmt.Sprintf("%s:%d", deps.Config.ServerHost, deps.Config.ServerPort)),
		wish.WithHostKeyPEM(key.RawPrivateKey()),
		wish.WithPublicKeyAuth(func(_ ssh.Context, _ ssh.PublicKey) bool {
			return true
		}),
		wish.WithMiddleware(
			RateLimitMiddleware(rateLimiter),
			func(sh ssh.Handler) ssh.Handler {
				return func(s ssh.Session) {
					defer func() {
						if r := recover(); r != nil {
							slog.Error("critical panic recovered during ssh session", "panic", r, "user", s.User(), "remote_addr", s.RemoteAddr().String())
							wish.Fatalf(s, "\r\nAn unexpected internal error occurred. The administrators have been notified.\r\n")
						}

						if s.Context().Value("owns_connection") == true {
							if u, ok := s.Context().Value("user").(*db.User); ok {
								tracker.Disconnect(u.ID)
								p := &player.Player{ID: fmt.Sprint(u.ID), DatabaseUser: u}
								deps.LobbyManager.LeaveLobby(p)
							}
						}
					}()

					sh(s)
				}
			},

			bm.Middleware(func(s ssh.Session) (tea.Model, []tea.ProgramOption) {
				fingerprint, err := AuthenticateSession(s)
				if err != nil {
					wish.Fatalf(s, "%v\n", err)
					return nil, nil
				}
				user, err := LoadOrRegisterUser(s.Context(), deps.UserRepository, s.User(), fingerprint)
				if err != nil {
					wish.Fatalf(s, "%v\n", err)
					return nil, nil
				}

				if !tracker.Connect(user.ID) {
					wish.Fatalf(s, "Account '%s' is already connected from another session.\n", user.Username)
					return nil, nil
				}

				s.Context().SetValue("owns_connection", true)
				s.Context().SetValue("user", user)

				return tui.Model(*user, deps.UserRepository, deps.MatchRepository, deps.LobbyManager, deps.GameRegistry), []tea.ProgramOption{
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
