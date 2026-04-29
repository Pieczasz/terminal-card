// Package lobby contains implementation of game lobby - it is responsible for
// random game code generations, players' game invitations, starting the game,
// selecting card game and options.
package lobby

import (
	"client/internal/db"
	"client/internal/player"
	"crypto/rand"
	"errors"
	"log/slog"
	"math/big"
	"slices"
	"sync"
)

type Lobby struct {
	mu      sync.RWMutex
	leader  *player.Player
	guests  []*player.Player
	options *options
	code    string
	state   state
}

type state uint

const (
	Waiting state = iota
	Closed
	InGame
)

type Manager struct {
	mu      sync.RWMutex
	lobbies map[string]*Lobby
}

type Option func(*options)

type options struct {
	cardGame   *db.Game
	maxPlayers int
	isPrivate  bool
}

func WithCardGame(game *db.Game) Option {
	return func(o *options) {
		o.cardGame = game
	}
}

func WithMaxPlayers(max int) Option {
	return func(o *options) {
		o.maxPlayers = max
	}
}

func WithPrivate(isPrivate bool) Option {
	return func(o *options) {
		o.isPrivate = isPrivate
	}
}

func NewManager() *Manager {
	return &Manager{
		lobbies: make(map[string]*Lobby),
	}
}

func (m *Manager) NewLobby(leader *player.Player, opts ...Option) (*Lobby, error) {
	options := &options{
		maxPlayers: 4,
		isPrivate:  false,
	}

	for _, opt := range opts {
		opt(options)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var code string
	maxRetries := 10
	for range maxRetries {
		code = generateLobbyCode(6)
		if _, exists := m.lobbies[code]; !exists {
			break
		}
		code = ""
	}

	if code == "" {
		slog.Error("failed to generate unique lobby code after maximum retries, how tf we ended up here")
		return nil, errors.New("unexpected error happen, please try again")
	}

	lobby := &Lobby{
		leader:  leader,
		guests:  make([]*player.Player, 0, options.maxPlayers-1),
		options: options,
		code:    code,
	}

	m.lobbies[code] = lobby
	return lobby, nil
}

func (m *Manager) FindLobbyByCode(code string) (*Lobby, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	lobby, exists := m.lobbies[code]
	if !exists {
		return nil, errors.New("lobby not found")
	}

	return lobby, nil
}

func (m *Manager) JoinLobbyByCode(code string, player *player.Player) error {
	lobby, err := m.FindLobbyByCode(code)
	if err != nil {
		return err
	}

	return lobby.addGuest(player)
}

func (l *Lobby) RemovePlayer(player *player.Player) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.leader.Compare(player) {
		if len(l.guests) > 0 {
			l.leader = l.guests[0]
			l.guests[0] = nil
			l.guests = l.guests[1:]
		} else {
			return true
		}
		return false
	}

	if idx := slices.Index(l.guests, player); idx != -1 {
		l.guests = slices.Delete(l.guests, idx, idx+1)
	}

	return false
}

func (m *Manager) RemoveLobby(code string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.lobbies, code)
}

func (l *Lobby) addGuest(player *player.Player) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if 1+len(l.guests) >= l.options.maxPlayers {
		return errors.New("this lobby is full")
	}

	if l.leader.Compare(player) {
		return errors.New("player is already the leader of this lobby")
	}
	if slices.Contains(l.guests, player) {
		return errors.New("player is already in this lobby")
	}

	l.guests = append(l.guests, player)
	return nil
}

func (l *Lobby) Code() string {
	return l.code
}

const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// generateLobbyCode creates a random string of the specified length.
func generateLobbyCode(length int) string {
	code := make([]byte, length)
	charsetLen := big.NewInt(int64(len(charset)))
	for i := range code {
		n, _ := rand.Int(rand.Reader, charsetLen)
		code[i] = charset[n.Int64()]
	}
	return string(code)
}
