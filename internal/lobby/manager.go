package lobby

import (
	"crypto/rand"
	"errors"
	"log/slog"
	"math/big"
	"slices"
	"sync"
	"time"

	"terminalcard/internal/broadcaster"
	"terminalcard/internal/db"
	"terminalcard/internal/player"
)

type Manager struct {
	mu                  sync.RWMutex
	lobbies             map[string]*Lobby
	playerLobby         map[string]*Lobby
	cachedPublicLobbies []*Lobby
	cacheLastUpdated    time.Time
	matchRepo           db.MatchRepository
}

func NewManager(matchRepo db.MatchRepository) *Manager {
	return &Manager{
		lobbies:     make(map[string]*Lobby),
		playerLobby: make(map[string]*Lobby),
		matchRepo:   matchRepo,
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
		manager:     m,
		leader:      leader,
		guests:      make([]*player.Player, 0, options.maxPlayers-1),
		options:     options,
		code:        code,
		ready:       make(map[string]bool),
		broadcaster: broadcaster.New[Event](options.maxPlayers),
	}

	m.lobbies[code] = lobby
	m.playerLobby[leader.ID] = lobby
	return lobby, nil
}

func (m *Manager) PublicLobbies(p *player.Player) []*Lobby {
	lobbies := m.getCachedPublicLobbies()
	if p != nil && p.DatabaseUser != nil {
		playerElos := make(map[string]uint32)
		for _, r := range p.DatabaseUser.Rankings {
			if r.Game.Name != "" {
				playerElos[r.Game.Name] = r.Elo
			}
		}
		slices.SortFunc(lobbies, func(a, b *Lobby) int {
			eloA := playerElos[a.GameName()]
			if eloA == 0 {
				eloA = 1500
			}
			eloB := playerElos[b.GameName()]
			if eloB == 0 {
				eloB = 1500
			}
			distA := abs(int(a.averageElo()) - int(eloA))
			distB := abs(int(b.averageElo()) - int(eloB))
			return distA - distB
		})
	}
	return lobbies
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
		m.mu.Lock()
		if m.playerLobby[player.ID] == lobby {
			delete(m.playerLobby, player.ID)
		}
		m.mu.Unlock()
	}
	return nil
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

func (m *Manager) getCachedPublicLobbies() []*Lobby {
	m.mu.RLock()
	if time.Since(m.cacheLastUpdated) < 2*time.Second {
		lobbies := make([]*Lobby, len(m.cachedPublicLobbies))
		copy(lobbies, m.cachedPublicLobbies)
		m.mu.RUnlock()
		return lobbies
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	if time.Since(m.cacheLastUpdated) < 2*time.Second {
		lobbies := make([]*Lobby, len(m.cachedPublicLobbies))
		copy(lobbies, m.cachedPublicLobbies)
		return lobbies
	}

	var publicLobbies []*Lobby
	for _, l := range m.lobbies {
		if !l.IsPrivate() && l.state == Waiting {
			publicLobbies = append(publicLobbies, l)
		}
	}

	m.cachedPublicLobbies = publicLobbies
	m.cacheLastUpdated = time.Now()

	lobbies := make([]*Lobby, len(publicLobbies))
	copy(lobbies, publicLobbies)
	return lobbies
}
