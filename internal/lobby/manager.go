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
	"sync/atomic"
	"time"

	"github.com/Pieczasz/terminal-card/internal/broadcaster"
	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/ratelimit"
)

const (
	lobbyCodeLength       = 8
	rankedFinalizeTimeout = 15 * time.Second
	joinRateLimitCount    = 10
	joinRateLimitWindow   = time.Second
	// maxLobbySubscribers is deliberately not maxPlayers: that setting is raised by
	// SetMaxPlayers long after the broadcaster exists, and a reconnecting player
	// briefly holds two subscriptions, so a table sized to its seats hands
	// ErrAtCapacity - a lobby view that never updates - to a player who joined
	// legitimately. This is the largest roster any game allows plus the same
	// headroom the engine keeps, which no raise can outgrow.
	maxLobbySubscribers = 10 + 8
)

var lobbyCodePattern = regexp.MustCompile(`^[A-Z0-9]{8}$`)

type Manager struct {
	mu                  sync.RWMutex
	lobbies             map[string]*Lobby
	playerLobby         map[string]*Lobby
	cachedPublicLobbies []*Lobby
	cacheLastUpdated    time.Time
	// cacheDirty is set by anything that changes which lobbies are public, a lobby
	// marking itself in-game included. It is atomic rather than guarded by m.mu: a
	// lobby sets it while holding its own lock, and reaching for the manager lock
	// there would invert the documented manager-then-lobby order.
	cacheDirty  atomic.Bool
	matchRepo   db.MatchRepository
	appCtx      context.Context
	joinLimiter *ratelimit.SlidingWindowLimiter
	// finalizerMu makes accepting a finished-match write and stopping new writes
	// atomic with respect to shutdown. WaitGroup alone permits Add after Wait
	// observes zero.
	finalizerMu       sync.Mutex
	finalizersStopped bool
	finalizing        sync.WaitGroup
}

func NewManager(ctx context.Context, matchRepo db.MatchRepository) *Manager {
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

func (m *Manager) playerInLobbyLocked(p *game.Player) bool {
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

func (m *Manager) New(leader *game.Player, opts ...Option) (*Lobby, error) {
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
		guests:      nil,
		options:     options,
		code:        code,
		ready:       make(map[string]bool),
		broadcaster: broadcaster.New[Event](maxLobbySubscribers),
		playerSubs:  make(map[string][]<-chan Event),
	}

	m.lobbies[code] = lobby
	m.playerLobby[leader.ID] = lobby
	m.invalidatePublicCache()
	return lobby, nil
}

func (m *Manager) JoinLobbyByCode(code string, p *game.Player) error {
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

func (m *Manager) FindLobbyByPlayer(p *game.Player) *Lobby {
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

func (m *Manager) LeaveLobby(p *game.Player) {
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

// registerFinalizer accepts a finished-match write unless shutdown has started.
func (m *Manager) registerFinalizer() bool {
	m.finalizerMu.Lock()
	defer m.finalizerMu.Unlock()
	if m.finalizersStopped {
		return false
	}
	m.finalizing.Add(1)
	return true
}

// WaitForFinalizers stops accepting finished-match writes, then blocks until all
// previously registered writes finish or timeout elapses. A non-positive timeout
// waits indefinitely.
func (m *Manager) WaitForFinalizers(timeout time.Duration) bool {
	if m == nil {
		return true
	}
	m.finalizerMu.Lock()
	m.finalizersStopped = true
	m.finalizerMu.Unlock()

	drained := make(chan struct{})
	go func() {
		m.finalizing.Wait()
		close(drained)
	}()
	if timeout <= 0 {
		<-drained
		return true
	}
	select {
	case <-drained:
		return true
	case <-time.After(timeout):
		return false
	}
}

// Stats counts lobbies by state for the public stats endpoint: how many hands are
// being played, and how many tables are sitting open waiting for players.
//
// The lobby set is copied under m.mu and the lock released before any lobby is
// touched, so each l.mu is taken on its own rather than nested inside m.mu. Reading
// l.state without l.mu would race with a lobby mutating itself.
func (m *Manager) Stats() (inGame, waiting int) {
	if m == nil {
		return 0, 0
	}
	m.mu.RLock()
	lobbies := make([]*Lobby, 0, len(m.lobbies))
	for _, l := range m.lobbies {
		lobbies = append(lobbies, l)
	}
	m.mu.RUnlock()

	for _, l := range lobbies {
		l.mu.RLock()
		switch l.state {
		case InGame:
			inGame++
		case Waiting:
			waiting++
		case Closed:
		}
		l.mu.RUnlock()
	}
	return inGame, waiting
}

// Kick removes a guest from the lobby. Only the current leader may kick guests.
func (m *Manager) Kick(host, target *game.Player) error {
	if host == nil || target == nil {
		return errors.New("host and target are required")
	}
	if host.Equal(target) {
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
	if !l.leader.Equal(host) {
		l.mu.Unlock()
		m.mu.Unlock()
		return errors.New("only the leader can kick players")
	}
	if l.leader.Equal(target) {
		l.mu.Unlock()
		m.mu.Unlock()
		return errors.New("cannot kick the lobby leader")
	}
	idx := slices.IndexFunc(l.guests, func(g *game.Player) bool { return g.Equal(target) })
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
	m.invalidatePublicCache()

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

// publicLobbyCacheTTL is what keeps a browse off every lobby's own lock. The window
// is short enough that a new table shows up on the next refresh.
const publicLobbyCacheTTL = 2 * time.Second

// invalidatePublicCache makes the next browse re-scan. Callers are the writes that
// change which tables are on offer, so a new or closed table shows up immediately
// rather than a cache window later.
func (m *Manager) invalidatePublicCache() {
	if m != nil {
		m.cacheDirty.Store(true)
	}
}

func (m *Manager) getCachedPublicLobbies() []*Lobby {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.cacheDirty.Load() && time.Since(m.cacheLastUpdated) < publicLobbyCacheTTL {
		lobbies := slices.Clone(m.cachedPublicLobbies)
		return lobbies
	}
	m.cacheDirty.Store(false)

	publicLobbies := make([]*Lobby, 0, len(m.lobbies))
	for _, l := range m.lobbies {
		if !l.IsPrivate() && l.IsWaiting() {
			publicLobbies = append(publicLobbies, l)
		}
	}

	m.cachedPublicLobbies = publicLobbies
	m.cacheLastUpdated = time.Now()

	lobbies := slices.Clone(publicLobbies)
	return lobbies
}
