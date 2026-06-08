// Package lobby contains implementation of game lobby - it is responsible for
// random game code generations, players' game invitations, starting the game,
// selecting card game and options.
package lobby

import (
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"slices"
	"sync"
	"terminalcard/internal/broadcaster"
	"terminalcard/internal/db"
	"terminalcard/internal/game"
	"terminalcard/internal/player"
)

type Lobby struct {
	mu           sync.RWMutex
	broadcaster  *broadcaster.Broadcaster[Event]
	leader       *player.Player
	guests       []*player.Player
	options      *options
	code         string
	state        state
	activeEngine *game.Engine
}

type Event struct {
	Type    string
	Payload any
}

type state uint

const (
	Waiting state = iota
	Closed
	InGame
)

type Manager struct {
	mu          sync.RWMutex
	lobbies     map[string]*Lobby
	playerLobby map[string]*Lobby
}

type Option func(*options)

type options struct {
	cardGame   *db.Game
	maxPlayers int
	isPrivate  bool
	isRanked   bool
}

func WithCardGame(game *db.Game) Option {
	return func(o *options) {
		o.cardGame = game
	}
}

func WithMaxPlayers(limit int) Option {
	return func(o *options) {
		o.maxPlayers = limit
	}
}

func WithPrivate(isPrivate bool) Option {
	return func(o *options) {
		o.isPrivate = isPrivate
	}
}

func WithRanked(isRanked bool) Option {
	return func(o *options) {
		o.isRanked = isRanked
	}
}

func (l *Lobby) SetPrivate(isPrivate bool) {
	l.mu.Lock()
	l.options.isPrivate = isPrivate
	l.mu.Unlock()
	if l.broadcaster != nil {
		l.broadcaster.Broadcast(Event{Type: "SETTINGS_UPDATED"})
	}
}

func (l *Lobby) SetMaxPlayers(limit int) {
	l.mu.Lock()
	l.options.maxPlayers = limit
	l.mu.Unlock()
	if l.broadcaster != nil {
		l.broadcaster.Broadcast(Event{Type: "SETTINGS_UPDATED"})
	}
}

func (l *Lobby) SetCardGame(game *db.Game) {
	l.mu.Lock()
	l.options.cardGame = game
	l.mu.Unlock()
	if l.broadcaster != nil {
		l.broadcaster.Broadcast(Event{Type: "SETTINGS_UPDATED"})
	}
}

func NewManager() *Manager {
	return &Manager{
		lobbies:     make(map[string]*Lobby),
		playerLobby: make(map[string]*Lobby),
	}
}

func (m *Manager) New(leader *player.Player, opts ...Option) (*Lobby, error) {
	if m.FindLobbyByPlayer(leader) != nil {
		return nil, errors.New("player is already in a lobby")
	}

	options := setupDefaultOptions()

	for _, opt := range opts {
		opt(options)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	code, err := m.generateLobbyCode()
	if err != nil {
		return nil, err
	}

	lobby := &Lobby{
		leader:      leader,
		guests:      make([]*player.Player, 0, options.maxPlayers-1),
		options:     options,
		code:        code,
		broadcaster: broadcaster.New[Event](options.maxPlayers),
	}

	m.lobbies[code] = lobby
	m.playerLobby[leader.ID] = lobby
	return lobby, nil
}

func setupDefaultOptions() *options {
	return &options{
		maxPlayers: 4,
		isPrivate:  true,
		isRanked:   true,
	}
}

const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func (m *Manager) generateLobbyCode() (string, error) {
	var code string
	maxAttempts := 10

	for range maxAttempts {
		rawCode := make([]byte, 6)
		charsetLen := big.NewInt(int64(len(charset)))
		for i := range rawCode {
			n, _ := rand.Int(rand.Reader, charsetLen)
			rawCode[i] = charset[n.Int64()]
		}
		code = string(rawCode)
		if _, exists := m.lobbies[code]; !exists {
			return code, nil
		}
	}
	slog.Error("failed to generate unique lobby code after maximum attempts", "attempts", maxAttempts)
	return "", errors.New("failed to generate lobby code")
}

func (l *Lobby) Code() string                                 { return l.code }
func (l *Lobby) Broadcaster() *broadcaster.Broadcaster[Event] { return l.broadcaster }
func (l *Lobby) GameName() string                             { return l.options.cardGame.Name }
func (l *Lobby) MaxPlayers() int                              { return l.options.maxPlayers }
func (l *Lobby) IsPrivate() bool                              { return l.options.isPrivate }
func (l *Lobby) Leader() *player.Player                       { return l.leader }

func (l *Lobby) Guests() []*player.Player {
	l.mu.RLock()
	defer l.mu.RUnlock()
	guests := make([]*player.Player, len(l.guests))
	copy(guests, l.guests)
	return guests
}

func (l *Lobby) CurrentPlayers() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return 1 + len(l.guests)
}

// TODO: optimize this.
func (m *Manager) PublicLobbies() []*Lobby {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var publicLobbies []*Lobby
	for _, l := range m.lobbies {
		if !l.IsPrivate() && l.state == Waiting {
			publicLobbies = append(publicLobbies, l)
		}
	}
	return publicLobbies
}

func (m *Manager) JoinLobbyByCode(code string, player *player.Player) error {
	if m.FindLobbyByPlayer(player) != nil {
		return errors.New("player is already in a lobby")
	}

	lobby, err := m.FindLobbyByCode(code)
	if err != nil {
		return err
	}

	err = lobby.addGuest(player)
	if err == nil {
		m.mu.Lock()
		m.playerLobby[player.ID] = lobby
		m.mu.Unlock()
	}
	return err
}

func (m *Manager) FindLobbyByPlayer(player *player.Player) *Lobby {
	m.mu.RLock()
	lobby, exists := m.playerLobby[player.ID]
	m.mu.RUnlock()

	if exists {
		if lobby.HasPlayer(player) {
			return lobby
		}
		// Lazy cleanup if player was removed (e.g. kicked)
		m.mu.Lock()
		if m.playerLobby[player.ID] == lobby {
			delete(m.playerLobby, player.ID)
		}
		m.mu.Unlock()
	}
	return nil
}

func (l *Lobby) HasPlayer(p *player.Player) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.leader.Compare(p) {
		return true
	}
	for _, g := range l.guests {
		if g.Compare(p) {
			return true
		}
	}
	return false
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

func (l *Lobby) RemovePlayer(p *player.Player) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.activeEngine != nil {
		l.activeEngine.RemovePlayer(p.ID)
	}

	if l.leader.Compare(p) {
		if len(l.guests) > 0 {
			l.leader = l.guests[0]
			l.guests = l.guests[1:]
			if l.broadcaster != nil {
				l.broadcaster.Broadcast(Event{Type: "PLAYERS_UPDATED"})
			}
			return false
		}

		if l.broadcaster != nil {
			l.broadcaster.Broadcast(Event{Type: "LOBBY_CLOSED"})
		}
		return true
	}

	if idx := slices.IndexFunc(l.guests, func(g *player.Player) bool { return g.Compare(p) }); idx != -1 {
		l.guests = slices.Delete(l.guests, idx, idx+1)
		if l.broadcaster != nil {
			l.broadcaster.Broadcast(Event{Type: "PLAYERS_UPDATED"})
		}
	}

	return false
}

func (m *Manager) LeaveLobby(p *player.Player) {
	l := m.FindLobbyByPlayer(p)
	if l == nil {
		return
	}

	if l.RemovePlayer(p) {
		m.RemoveLobby(l.Code())
	} else {
		m.mu.Lock()
		delete(m.playerLobby, p.ID)
		m.mu.Unlock()
	}
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

	delete(m.playerLobby, l.leader.ID)
	for _, g := range l.guests {
		delete(m.playerLobby, g.ID)
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
	if l.broadcaster != nil {
		l.broadcaster.Broadcast(Event{Type: "PLAYERS_UPDATED"})
	}
	return nil
}

func (l *Lobby) StartGame(registry *game.Registry) (*game.Engine, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.state != Waiting {
		return nil, errors.New("lobby is not in waiting state")
	}

	rules, err := registry.Create(l.options.cardGame.Name)
	if err != nil {
		slog.Error("how did we end up here, user selected a game that doesn't exist", "error", err)
		return nil, fmt.Errorf("failed to create game rules: %w", err)
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

	if err := engine.Start(); err != nil {
		return nil, fmt.Errorf("failed to start game engine: %w", err)
	}

	l.state = InGame
	l.activeEngine = engine

	l.broadcaster.Broadcast(Event{
		Type:    "GAME_STARTED",
		Payload: engine,
	})

	return engine, nil
}
