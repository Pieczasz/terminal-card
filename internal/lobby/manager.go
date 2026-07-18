package lobby

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/Pieczasz/terminal-card/internal/broadcaster"
	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/elo"
	"github.com/Pieczasz/terminal-card/internal/player"
	"github.com/Pieczasz/terminal-card/internal/ratelimit"
)

const (
	lobbyCodeLength       = 8
	rankedFinalizeTimeout = 15 * time.Second
	joinRateLimitCount    = 10
	joinRateLimitWindow   = time.Second
)

var lobbyCodePattern = regexp.MustCompile(`^[A-Z0-9]{8}$`)

type Manager struct {
	mu                  sync.RWMutex
	lobbies             map[string]*Lobby
	playerLobby         map[string]*Lobby
	cachedPublicLobbies []*Lobby
	cacheLastUpdated    time.Time
	matchRepo           db.MatchRepository
	appCtx              context.Context
	joinLimiter         *ratelimit.SlidingWindowLimiter
}

func NewManager(matchRepo db.MatchRepository) *Manager {
	return NewManagerWithContext(context.Background(), matchRepo)
}

func NewManagerWithContext(ctx context.Context, matchRepo db.MatchRepository) *Manager {
	if ctx == nil {
		ctx = context.Background()
	}
	return &Manager{
		lobbies:     make(map[string]*Lobby),
		playerLobby: make(map[string]*Lobby),
		matchRepo:   matchRepo,
		appCtx:      ctx,
		joinLimiter: ratelimit.NewSlidingWindowLimiter(joinRateLimitCount, joinRateLimitWindow),
	}
}

const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func (m *Manager) generateLobbyCode() (string, error) {
	var code string
	maxAttempts := 10

	for range maxAttempts {
		rawCode := make([]byte, lobbyCodeLength)
		charsetLen := big.NewInt(int64(len(charset)))
		for i := range rawCode {
			n, err := rand.Int(rand.Reader, charsetLen)
			if err != nil {
				return "", fmt.Errorf("generating lobby code: %w", err)
			}
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

func ValidLobbyCode(code string) bool {
	return lobbyCodePattern.MatchString(code)
}

func (m *Manager) playerInLobbyLocked(p *player.Player) bool {
	lobby, exists := m.playerLobby[p.ID]
	if !exists {
		return false
	}
	if lobby.HasPlayer(p) {
		return true
	}
	delete(m.playerLobby, p.ID)
	return false
}

func (m *Manager) New(leader *player.Player, opts ...Option) (*Lobby, error) {
	options := setupDefaultOptions()
	for _, opt := range opts {
		opt(options)
	}

	if options.cardGame == nil || options.cardGame.Name == "" {
		return nil, errors.New("card game is required")
	}
	if options.maxPlayers < 2 {
		return nil, errors.New("max players must be at least 2")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.playerInLobbyLocked(leader) {
		return nil, errors.New("player is already in a lobby")
	}

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
		playerSubs:  make(map[string][]<-chan Event),
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
				eloA = elo.ToUint32(elo.DefaultRating)
			}
			eloB := playerElos[b.GameName()]
			if eloB == 0 {
				eloB = elo.ToUint32(elo.DefaultRating)
			}
			distA := abs(int(a.averageElo()) - int(eloA))
			distB := abs(int(b.averageElo()) - int(eloB))
			return distA - distB
		})
	}
	return lobbies
}

func (m *Manager) JoinLobbyByCode(code string, p *player.Player) error {
	if !ValidLobbyCode(code) {
		return errors.New("invalid lobby code")
	}
	if m.joinLimiter != nil && !m.joinLimiter.Allow("join:"+p.ID) {
		return errors.New("too many join attempts, please try again later")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.playerInLobbyLocked(p) {
		return errors.New("player is already in a lobby")
	}

	lobby, exists := m.lobbies[code]
	if !exists {
		return errors.New("lobby not found")
	}

	if err := lobby.addGuest(p); err != nil {
		return err
	}
	m.playerLobby[p.ID] = lobby
	return nil
}

func (m *Manager) FindLobbyByPlayer(p *player.Player) *Lobby {
	m.mu.RLock()
	lobby, exists := m.playerLobby[p.ID]
	m.mu.RUnlock()

	if !exists {
		return nil
	}
	if lobby.HasPlayer(p) {
		return lobby
	}
	m.mu.Lock()
	if m.playerLobby[p.ID] == lobby {
		delete(m.playerLobby, p.ID)
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) FindLobbyByCode(code string) (*Lobby, error) {
	if !ValidLobbyCode(code) {
		return nil, errors.New("invalid lobby code")
	}
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

func (m *Manager) shutdownCtx() context.Context {
	if m == nil || m.appCtx == nil {
		return context.Background()
	}
	return m.appCtx
}

// Kick removes a guest from the lobby. Only the current leader may kick guests.
func (m *Manager) Kick(host, target *player.Player) error {
	if host == nil || target == nil {
		return errors.New("host and target are required")
	}
	if host.Compare(target) {
		return errors.New("cannot kick yourself")
	}

	m.mu.Lock()
	l, ok := m.playerLobby[host.ID]
	if !ok || l == nil {
		m.mu.Unlock()
		return errors.New("host is not in a lobby")
	}
	// Hold manager lock while mutating lobby so join/leave cannot interleave.
	l.mu.Lock()
	if l.state == Closed {
		l.mu.Unlock()
		m.mu.Unlock()
		return errors.New("lobby is closed")
	}
	if !l.leader.Compare(host) {
		l.mu.Unlock()
		m.mu.Unlock()
		return errors.New("only the leader can kick players")
	}
	if l.leader.Compare(target) {
		l.mu.Unlock()
		m.mu.Unlock()
		return errors.New("cannot kick the lobby leader")
	}
	idx := slices.IndexFunc(l.guests, func(g *player.Player) bool { return g.Compare(target) })
	if idx == -1 {
		l.mu.Unlock()
		m.mu.Unlock()
		return errors.New("player not in lobby")
	}

	engine := l.activeEngine
	bc := l.broadcaster
	l.removeGuestAtLocked(idx)
	delete(m.playerLobby, target.ID)
	l.mu.Unlock()
	m.mu.Unlock()

	notifyEngineAndBroadcast(engine, bc, target.ID, EventPlayersUpdated)
	return nil
}

func (m *Manager) RemoveLobby(code string) {
	m.mu.Lock()
	l, exists := m.lobbies[code]
	if !exists {
		m.mu.Unlock()
		return
	}
	delete(m.lobbies, code)

	l.mu.Lock()
	leaderID := ""
	if l.leader != nil {
		leaderID = l.leader.ID
	}
	guestIDs := make([]string, len(l.guests))
	for i, g := range l.guests {
		guestIDs[i] = g.ID
	}
	engine := l.activeEngine
	l.activeEngine = nil
	bc := l.broadcaster
	l.broadcaster = nil
	l.mu.Unlock()

	if leaderID != "" {
		delete(m.playerLobby, leaderID)
	}
	for _, id := range guestIDs {
		delete(m.playerLobby, id)
	}
	m.mu.Unlock()

	if engine != nil {
		engine.Close()
	}
	if bc != nil {
		bc.Close()
	}
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
		if !l.IsPrivate() && l.IsWaiting() {
			publicLobbies = append(publicLobbies, l)
		}
	}

	m.cachedPublicLobbies = publicLobbies
	m.cacheLastUpdated = time.Now()

	lobbies := make([]*Lobby, len(publicLobbies))
	copy(lobbies, publicLobbies)
	return lobbies
}
