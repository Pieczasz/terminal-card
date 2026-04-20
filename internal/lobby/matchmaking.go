// Package lobby contains implementation of game lobby - it is responsible for
// random game code generations, players game invitations, starting the game,
// selecting card game and options.
package lobby

import (
	"client/internal/db"
	"crypto/rand"
	"errors"
	"log"
	"math/big"
	"slices"
	"sync"
)

// Lobby contains all information related to a lobby.
type Lobby struct {
	mu        sync.RWMutex
	leader    *db.User
	guests    []*db.User
	options   *lobbyOptions
	lobbyCode string
}

// Manager handles the lifecycle and retrieval of all active lobbies.
type Manager struct {
	mu      sync.RWMutex
	lobbies map[string]*Lobby
}

// Option defines a functional option for configuring a Lobby.
type Option func(*lobbyOptions)

// lobbyOptions holds the configuration for a lobby.
type lobbyOptions struct {
	cardGame   *db.Game
	maxPlayers int
	isPrivate  bool
}

// WithCardGame sets the card game for the lobby.
func WithCardGame(game *db.Game) Option {
	return func(o *lobbyOptions) {
		o.cardGame = game
	}
}

// WithMaxPlayers sets the maximum number of players for the lobby.
func WithMaxPlayers(max int) Option {
	return func(o *lobbyOptions) {
		o.maxPlayers = max
	}
}

// WithPrivate sets the privacy status of the lobby.
func WithPrivate(isPrivate bool) Option {
	return func(o *lobbyOptions) {
		o.isPrivate = isPrivate
	}
}

// NewManager creates and returns a new Lobby Manager.
func NewManager() *Manager {
	return &Manager{
		lobbies: make(map[string]*Lobby),
	}
}

// NewLobby creates a new lobby and registers it in the manager.
func (m *Manager) NewLobby(leader *db.User, opts ...Option) (*Lobby, error) {
	options := &lobbyOptions{
		maxPlayers: 4,
		isPrivate:  false,
	}

	for _, opt := range opts {
		opt(options)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate a unique lobby code with collision handling
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
		log.Print("failed to generate unique lobby code after maximum retries, how tf we ended up here")
		return nil, errors.New("unexpected error happen, please try again")
	}

	lobby := &Lobby{
		leader:    leader,
		guests:    make([]*db.User, 0, options.maxPlayers-1),
		options:   options,
		lobbyCode: code,
	}

	m.lobbies[code] = lobby
	return lobby, nil
}

// FindLobbyByCode retrieves a lobby by its lobby code. Throws an error if the lobby
// is not found.
func (m *Manager) FindLobbyByCode(code string) (*Lobby, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	lobby, exists := m.lobbies[code]
	if !exists {
		return nil, errors.New("lobby not found")
	}

	return lobby, nil
}

// JoinLobbyByCode attempts to add a user to an existing lobby by code.
func (m *Manager) JoinLobbyByCode(code string, player *db.User) error {
	lobby, err := m.FindLobbyByCode(code)
	if err != nil {
		return err
	}

	return lobby.addGuest(player)
}

// RemovePlayer removes a player from the lobby.
// It returns true if the lobby is now empty and should be deleted.
func (l *Lobby) RemovePlayer(player *db.User) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.leader == player {
		if len(l.guests) > 0 {
			l.leader = l.guests[0]
			l.guests = l.guests[1:]
		} else {
			return true
		}
	} else {
		l.guests = slices.DeleteFunc(l.guests, func(p *db.User) bool {
			return p == player
		})
	}

	return false
}

// RemoveLobby deletes a lobby from the manager.
func (m *Manager) RemoveLobby(code string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.lobbies, code)
}

// TODO: is user comparision by reference to db.User safe?
func (l *Lobby) addGuest(player *db.User) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if 1+len(l.guests) >= l.options.maxPlayers {
		return errors.New("this lobby is full")
	}

	if l.leader == player {
		return errors.New("player is already the leader of this lobby")
	}
	if slices.Contains(l.guests, player) {
		return errors.New("player is already in this lobby")
	}

	l.guests = append(l.guests, player)
	return nil
}

// Code returns the lobby's code
func (l *Lobby) Code() string {
	// I guess we dont need locks here since code is immutable.
	return l.lobbyCode
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
