// Package lobby contains implementation of game lobby - it is responsible for
// random game code generations, players' game invitations, starting the game,
// selecting card game and options.
package lobby

import (
	"client/internal/broadcaster"
	"client/internal/db"
	"client/internal/game"
	"client/internal/player"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"slices"
	"sync"
)

type LobbyEvent struct {
	Type string
	Data any
}

type Lobby struct {
	mu          sync.RWMutex
	broadcaster *broadcaster.Broadcaster[LobbyEvent]
	leader      *player.Player
	guests      []*player.Player
	options     *options
	code        string
	state       state
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
		leader:      leader,
		guests:      make([]*player.Player, 0, options.maxPlayers-1),
		options:     options,
		code:        code,
		broadcaster: broadcaster.New[LobbyEvent](options.maxPlayers),
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

	l, exists := m.lobbies[code]
	if !exists {
		return
	}

	l.mu.Lock()
	if l.broadcaster != nil {
		l.broadcaster.Close()
		l.broadcaster = nil
	}
	l.mu.Unlock()

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

func (l *Lobby) Broadcaster() *broadcaster.Broadcaster[LobbyEvent] {
	return l.broadcaster
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

func (l *Lobby) StartGame(registry *game.Registry) (*game.Engine, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.state != Waiting {
		return nil, errors.New("lobby is not in waiting state")
	}

	rules, err := registry.Create(l.options.cardGame.Name)
	if err != nil {
		return nil, err
	}

	totalPlayers := len(l.guests) + 1
	if totalPlayers < rules.MinPlayers() {
		return nil, fmt.Errorf("need at least %d players to start", rules.MinPlayers())
	}
	if totalPlayers > rules.MaxPlayers() {
		return nil, fmt.Errorf("too many players for this game")
	}

	players := append([]*player.Player{l.leader}, l.guests...)
	engine := game.NewGameEngine(rules, players, rules.InitialDeck())

	l.state = InGame

	l.broadcaster.Broadcast(LobbyEvent{
		Type: "GAME_STARTED",
	})

	return engine, nil
}
