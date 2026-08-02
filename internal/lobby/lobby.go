package lobby

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/Pieczasz/terminal-card/internal/broadcaster"
	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/elo"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/observability"
	"github.com/Pieczasz/terminal-card/internal/player"
)

type Lobby struct {
	mu           sync.RWMutex
	manager      *Manager
	broadcaster  *broadcaster.Broadcaster[Event]
	leader       *player.Player
	guests       []*player.Player
	options      *options
	code         string
	state        state
	ready        map[string]bool
	activeEngine *game.Engine
	playerSubs   map[string][]<-chan Event
}

type Event struct {
	Type    string
	Payload any
}

// Lobby event type constants shared with TUI subscribers.
const (
	EventPlayersUpdated  = "PLAYERS_UPDATED"
	EventSettingsUpdated = "SETTINGS_UPDATED"
	EventLobbyClosed     = "LOBBY_CLOSED"
	EventGameStarted     = "GAME_STARTED"
)

type state uint

const (
	Waiting state = iota
	Closed
	InGame
)

type Option func(*options)

type options struct {
	cardGame   *db.Game
	maxPlayers int
	isPrivate  bool
	isRanked   bool
}

func WithCardGame(g *db.Game) Option {
	return func(o *options) {
		o.cardGame = g
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

func setupDefaultOptions() *options {
	return &options{
		maxPlayers: 4,
		isPrivate:  true,
		isRanked:   false, // casual by default; leaders opt into ranked Elo
	}
}

func (l *Lobby) broadcastUnlocked(event Event) {
	if l.broadcaster != nil {
		l.broadcaster.Broadcast(event)
	}
}

func (l *Lobby) takeBroadcaster() *broadcaster.Broadcaster[Event] {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.broadcaster
}

// SetPrivate updates lobby visibility. Only the current leader may change settings.
func (l *Lobby) SetPrivate(actor *player.Player, isPrivate bool) error {
	return l.withLeaderSettings(actor, func() error {
		l.options.isPrivate = isPrivate
		return nil
	})
}

// SetRanked updates whether the lobby writes Elo on finish. Leader-only while waiting.
func (l *Lobby) SetRanked(actor *player.Player, isRanked bool) error {
	return l.withLeaderSettings(actor, func() error {
		l.options.isRanked = isRanked
		return nil
	})
}

// SetMaxPlayers updates capacity. Clamped to current roster size and optional game rules bounds.
func (l *Lobby) SetMaxPlayers(actor *player.Player, limit int, rulesMin, rulesMax int) error {
	return l.withLeaderSettings(actor, func() error {
		current := 1 + len(l.guests)
		if limit < current {
			return fmt.Errorf("max players cannot be below current roster (%d)", current)
		}
		if rulesMin > 0 && limit < rulesMin {
			return fmt.Errorf("max players must be at least %d for this game", rulesMin)
		}
		if rulesMax > 0 && limit > rulesMax {
			return fmt.Errorf("max players cannot exceed %d for this game", rulesMax)
		}
		l.options.maxPlayers = limit
		return nil
	})
}

// SetCardGame updates the selected game. Only the leader may change it while waiting.
func (l *Lobby) SetCardGame(actor *player.Player, g *db.Game) error {
	return l.withLeaderSettings(actor, func() error {
		l.options.cardGame = g
		return nil
	})
}

// withLeaderSettings runs mutate while holding the lobby lock after verifying the
// actor is the leader and the lobby is Waiting. Broadcasts SETTINGS_UPDATED on success.
func (l *Lobby) withLeaderSettings(actor *player.Player, mutate func() error) error {
	l.mu.Lock()
	if !l.leader.Equal(actor) {
		l.mu.Unlock()
		return errors.New("only the leader can change settings")
	}
	if l.state != Waiting {
		l.mu.Unlock()
		return errors.New("cannot change settings while a game is in progress")
	}
	if err := mutate(); err != nil {
		l.mu.Unlock()
		return err
	}
	bc := l.broadcaster
	l.mu.Unlock()
	if bc != nil {
		bc.Broadcast(Event{Type: EventSettingsUpdated})
	}
	return nil
}

// RemovePlayer removes a player. Returns true if the lobby should be closed (empty).
// Prefer Manager.LeaveLobby / Manager.Kick so playerLobby stays consistent.
func (l *Lobby) RemovePlayer(p *player.Player) bool {
	if p == nil {
		return false
	}
	l.mu.Lock()

	playerID := p.ID
	l.unsubscribePlayerLocked(playerID)

	engine, bc, eventType, shouldClose, ok := l.detachPlayerLocked(p)
	l.mu.Unlock()
	if !ok {
		return false
	}
	notifyEngineAndBroadcast(engine, bc, playerID, eventType)
	return shouldClose
}

// detachPlayerLocked mutates roster for a leaving player. Caller holds l.mu.
// Returns false in ok if the player was not in the lobby.
func (l *Lobby) detachPlayerLocked(p *player.Player) (
	engine *game.Engine,
	bc *broadcaster.Broadcaster[Event],
	eventType string,
	shouldClose, ok bool,
) {
	engine = l.activeEngine
	bc = l.broadcaster

	if l.leader.Equal(p) {
		if len(l.guests) > 0 {
			l.leader = l.guests[0]
			l.guests = l.guests[1:]
			delete(l.ready, p.ID)
			return engine, bc, EventPlayersUpdated, false, true
		}
		l.state = Closed
		return engine, bc, EventLobbyClosed, true, true
	}

	if idx := slices.IndexFunc(l.guests, func(g *player.Player) bool { return g.Equal(p) }); idx != -1 {
		l.removeGuestAtLocked(idx)
		return engine, bc, EventPlayersUpdated, false, true
	}
	return nil, nil, "", false, false
}

// removeGuestAtLocked removes guests[idx] and clears ready/subs. Caller holds l.mu.
func (l *Lobby) removeGuestAtLocked(idx int) {
	g := l.guests[idx]
	l.unsubscribePlayerLocked(g.ID)
	l.guests = slices.Delete(l.guests, idx, idx+1)
	delete(l.ready, g.ID)
}

func notifyEngineAndBroadcast(engine *game.Engine, bc *broadcaster.Broadcaster[Event], playerID, eventType string) {
	if engine != nil {
		engine.RemovePlayer(playerID)
	}
	if bc != nil && eventType != "" {
		bc.Broadcast(Event{Type: eventType})
	}
}

// Subscribe registers a lobby event channel for playerID so disconnect can unsubscribe.
func (l *Lobby) Subscribe(playerID string) <-chan Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.broadcaster == nil || l.state == Closed {
		ch := make(chan Event)
		close(ch)
		return ch
	}
	ch := l.broadcaster.Subscribe()
	if l.playerSubs == nil {
		l.playerSubs = make(map[string][]<-chan Event)
	}
	l.playerSubs[playerID] = append(l.playerSubs[playerID], ch)
	return ch
}

// Unsubscribe removes a single channel previously returned by Subscribe.
func (l *Lobby) Unsubscribe(playerID string, ch <-chan Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.broadcaster == nil || ch == nil {
		return
	}
	l.broadcaster.Unsubscribe(ch)
	if playerID == "" || l.playerSubs == nil {
		return
	}
	subs := l.playerSubs[playerID]
	for i, sub := range subs {
		if sub == ch {
			l.playerSubs[playerID] = slices.Delete(subs, i, i+1)
			if len(l.playerSubs[playerID]) == 0 {
				delete(l.playerSubs, playerID)
			}
			return
		}
	}
}

func (l *Lobby) unsubscribePlayerLocked(playerID string) {
	if playerID == "" || l.broadcaster == nil || l.playerSubs == nil {
		return
	}
	for _, ch := range l.playerSubs[playerID] {
		l.broadcaster.Unsubscribe(ch)
	}
	delete(l.playerSubs, playerID)
}

func (l *Lobby) ToggleReady(p *player.Player, registry *game.Registry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.state == InGame {
		if l.activeEngine != nil && l.activeEngine.IsFinished() {
			old := l.activeEngine
			l.state = Waiting
			l.activeEngine = nil
			clear(l.ready)
			old.Close()
		} else {
			return errors.New("game is already in progress")
		}
	}

	if !l.hasPlayerNoLock(p) {
		return errors.New("player not in lobby")
	}

	l.ready[p.ID] = !l.ready[p.ID]

	if !l.allReadyNoLock() {
		l.broadcastUnlocked(Event{Type: EventPlayersUpdated})
		return nil
	}

	_, err := l.startGameLocked(registry)
	return err
}

func (l *Lobby) allReadyNoLock() bool {
	if !l.ready[l.leader.ID] {
		return false
	}
	for _, g := range l.guests {
		if !l.ready[g.ID] {
			return false
		}
	}
	return true
}

func (l *Lobby) Code() string { return l.code }

func (l *Lobby) Broadcaster() *broadcaster.Broadcaster[Event] {
	return l.takeBroadcaster()
}

func (l *Lobby) GameName() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.options.cardGame == nil {
		return ""
	}
	return l.options.cardGame.Name
}

func (l *Lobby) MaxPlayers() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.options.maxPlayers
}

func (l *Lobby) IsRanked() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.options.isRanked
}

func (l *Lobby) IsPrivate() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.options.isPrivate
}

func (l *Lobby) IsWaiting() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.state == Waiting
}

func (l *Lobby) Leader() *player.Player {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.leader
}

func (l *Lobby) Guests() []*player.Player {
	l.mu.RLock()
	defer l.mu.RUnlock()
	guests := slices.Clone(l.guests)
	return guests
}

func (l *Lobby) CurrentPlayers() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return 1 + len(l.guests)
}

func (l *Lobby) HasPlayer(p *player.Player) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.hasPlayerNoLock(p)
}

func (l *Lobby) IsReady(p *player.Player) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.ready[p.ID]
}

func (l *Lobby) IsLeader(p *player.Player) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.leader.Equal(p)
}

func playerEloForGame(p *player.Player, gameName string) uint32 {
	if p == nil || p.DatabaseUser == nil {
		return elo.ToUint32(elo.DefaultRating)
	}
	for _, r := range p.DatabaseUser.Rankings {
		if r.Game.Name == gameName {
			return r.Elo
		}
	}
	return elo.ToUint32(elo.DefaultRating)
}

func (l *Lobby) averageElo() uint32 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.options.cardGame == nil {
		return elo.ToUint32(elo.DefaultRating)
	}
	gameName := l.options.cardGame.Name
	var totalElo uint32
	var count uint32

	totalElo += playerEloForGame(l.leader, gameName)
	count++
	for _, g := range l.guests {
		totalElo += playerEloForGame(g, gameName)
		count++
	}
	return totalElo / count
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func (l *Lobby) addGuest(p *player.Player) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.state != Waiting {
		return errors.New("lobby is not accepting players")
	}

	if 1+len(l.guests) >= l.options.maxPlayers {
		return errors.New("this lobby is full")
	}

	if l.leader.Equal(p) {
		return errors.New("player is already the leader of this lobby")
	}
	for _, g := range l.guests {
		if g.Equal(p) {
			return errors.New("player is already in this lobby")
		}
	}

	l.guests = append(l.guests, p)
	l.broadcastUnlocked(Event{Type: EventPlayersUpdated})
	return nil
}

func (l *Lobby) hasPlayerNoLock(p *player.Player) bool {
	if l.leader.Equal(p) {
		return true
	}
	for _, g := range l.guests {
		if g.Equal(p) {
			return true
		}
	}
	return false
}

// startGameLocked starts a match. Caller must hold l.mu.
func (l *Lobby) startGameLocked(registry *game.Registry) (*game.Engine, error) {
	if l.state != Waiting {
		return nil, errors.New("lobby is not in waiting state")
	}
	if !l.allReadyNoLock() {
		return nil, errors.New("not all players are ready")
	}
	if l.options.cardGame == nil {
		return nil, errors.New("no card game selected")
	}

	rules, err := registry.Create(l.options.cardGame.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to create game rules: %w", err)
	}

	totalPlayers := len(l.guests) + 1
	if totalPlayers < rules.MinPlayers() {
		return nil, fmt.Errorf("need at least %d players to start", rules.MinPlayers())
	}
	if totalPlayers > rules.MaxPlayers() {
		return nil, errors.New("too many players for this game")
	}

	players := slices.Concat([]*player.Player{l.leader}, l.guests)
	engine := game.NewEngine(rules, players, rules.InitialDeck())

	if err := engine.Start(); err != nil {
		return nil, fmt.Errorf("failed to start game engine: %w", err)
	}

	if l.manager != nil && l.manager.matchRepo != nil {
		ch := engine.Broadcaster().Subscribe()
		go func() {
			defer engine.Broadcaster().Unsubscribe(ch)
			l.handleBroadcasterEvents(ch, engine)
		}()
	}

	l.state = InGame
	l.activeEngine = engine
	clear(l.ready)

	observability.GamesStartedTotal.Add(1)

	l.broadcastUnlocked(Event{
		Type:    EventGameStarted,
		Payload: engine,
	})

	return engine, nil
}

func (l *Lobby) handleBroadcasterEvents(ch <-chan game.Event, engine *game.Engine) {
	for event := range ch {
		if event.Type == game.EventGameEnded {
			l.finalizeFinishedGame(engine)
			return
		}
	}
}

// finalizeFinishedGame persists the result of a game that just ended. It is its
// own function so the finalizing counter and the write context are released by
// defer: leaking either would leave shutdown waiting on a write that is over.
func (l *Lobby) finalizeFinishedGame(engine *game.Engine) {
	standings := engine.Standings()
	userIDs := make([]uint, 0, len(standings))
	for _, p := range standings {
		if p == nil || p.DatabaseUser == nil {
			slog.Error("standing player missing database user; skipping ranked finalize")
			return
		}
		userIDs = append(userIDs, p.DatabaseUser.ID)
	}

	l.mu.RLock()
	isRanked := l.options.isRanked
	gameName := ""
	if l.options.cardGame != nil {
		gameName = l.options.cardGame.Name
	}
	parentCtx := l.manager.shutdownCtx()
	l.mu.RUnlock()

	if gameName == "" || l.manager == nil || l.manager.matchRepo == nil {
		return
	}

	if !l.manager.registerFinalizer() {
		return
	}
	defer l.manager.finalizing.Done()
	ctx, cancel := context.WithTimeout(parentCtx, rankedFinalizeTimeout)
	defer cancel()

	if err := l.recordFinishedMatch(ctx, gameName, userIDs, isRanked); err != nil {
		slog.Error("failed to record finished match", "error", err, "game", gameName, "ranked", isRanked)
	}
}

// recordFinishedMatch writes match history for every finished game. Only a ranked
// lobby also moves Elo; a casual one still belongs in the players' history.
func (l *Lobby) recordFinishedMatch(ctx context.Context, gameName string, userIDs []uint, isRanked bool) error {
	repo := l.manager.matchRepo
	if isRanked {
		if err := repo.FinalizeRankedMatch(ctx, gameName, userIDs); err != nil {
			return fmt.Errorf("finalize ranked match: %w", err)
		}
		return nil
	}
	g, err := repo.GetOrCreateGame(ctx, gameName)
	if err != nil {
		return fmt.Errorf("resolve game: %w", err)
	}
	// No Elo deltas: a casual result is history only, it must not move ratings.
	if err := repo.RecordMatch(ctx, g.ID, userIDs, nil, false); err != nil {
		return fmt.Errorf("record casual match: %w", err)
	}
	return nil
}
