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
)

type Lobby struct {
	mu           sync.RWMutex
	manager      *Manager
	broadcaster  *broadcaster.Broadcaster[Event]
	leader       *game.Player
	guests       []*game.Player
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
		isRanked:   false,
	}
}

func (l *Lobby) setStateLocked(s state) {
	l.state = s
	l.manager.invalidatePublicCache()
}

func (l *Lobby) broadcastLocked(event Event) {
	if l.broadcaster != nil {
		l.broadcaster.Broadcast(event)
	}
}

// SetPrivate updates lobby visibility. Only the current leader may change settings.
func (l *Lobby) SetPrivate(actor *game.Player, isPrivate bool) error {
	return l.withLeaderSettings(actor, func() error {
		l.options.isPrivate = isPrivate
		return nil
	})
}

// SetRanked updates whether the lobby writes Elo on finish. Leader-only while waiting.
func (l *Lobby) SetRanked(actor *game.Player, isRanked bool) error {
	return l.withLeaderSettings(actor, func() error {
		l.options.isRanked = isRanked
		return nil
	})
}

// SetMaxPlayers updates capacity. Clamped to current roster size and optional game rules bounds.
func (l *Lobby) SetMaxPlayers(actor *game.Player, limit int, rulesMin, rulesMax int) error {
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
func (l *Lobby) SetCardGame(actor *game.Player, g *db.Game) error {
	return l.withLeaderSettings(actor, func() error {
		l.options.cardGame = g
		return nil
	})
}

// withLeaderSettings runs mutate while holding the lobby lock after verifying the
// actor is the leader and the lobby is Waiting. Broadcasts SETTINGS_UPDATED on success.
func (l *Lobby) withLeaderSettings(actor *game.Player, mutate func() error) error {
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
	// Visibility is one of these settings, so a browse served from the cache would
	// keep offering a table that just went private, or hide one that just opened.
	l.manager.invalidatePublicCache()
	bc := l.broadcaster
	l.mu.Unlock()
	if bc != nil {
		bc.Broadcast(Event{Type: EventSettingsUpdated})
	}
	return nil
}

// RemovePlayer removes a player. Returns true if the lobby should be closed (empty).
// Prefer Manager.LeaveLobby / Manager.Kick so playerLobby stays consistent.
func (l *Lobby) RemovePlayer(p *game.Player) bool {
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
func (l *Lobby) detachPlayerLocked(p *game.Player) (
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
		l.setStateLocked(Closed)
		return engine, bc, EventLobbyClosed, true, true
	}

	if idx := slices.IndexFunc(l.guests, func(g *game.Player) bool { return g.Equal(p) }); idx != -1 {
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

// Subscribe registers a lobby event channel for playerID so disconnect can
// unsubscribe. An error means the caller will receive nothing and must say so
// rather than sitting on a channel that stays silent forever.
func (l *Lobby) Subscribe(playerID string) (<-chan Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.broadcaster == nil || l.state == Closed {
		return nil, errors.New("lobby is closed")
	}
	ch, err := l.broadcaster.Subscribe()
	if err != nil {
		return nil, fmt.Errorf("subscribe to lobby events: %w", err)
	}
	if l.playerSubs == nil {
		l.playerSubs = make(map[string][]<-chan Event)
	}
	l.playerSubs[playerID] = append(l.playerSubs[playerID], ch)
	return ch, nil
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

func (l *Lobby) ToggleReady(p *game.Player, registry *game.Registry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.state == InGame {
		finished := l.releaseFinishedGameLocked()
		if finished == nil {
			return errors.New("game is already in progress")
		}
		finished.Close()
	}

	if !l.hasPlayerLocked(p) {
		return errors.New("player not in lobby")
	}

	l.ready[p.ID] = !l.ready[p.ID]

	if !l.allReadyLocked() {
		l.broadcastLocked(Event{Type: EventPlayersUpdated})
		return nil
	}

	_, err := l.startGameLocked(registry)
	return err
}

func (l *Lobby) allReadyLocked() bool {
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
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.broadcaster
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

func (l *Lobby) Leader() *game.Player {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.leader
}

func (l *Lobby) Guests() []*game.Player {
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

func (l *Lobby) HasPlayer(p *game.Player) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.hasPlayerLocked(p)
}

func (l *Lobby) IsReady(p *game.Player) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.ready[p.ID]
}

func (l *Lobby) IsLeader(p *game.Player) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.leader.Equal(p)
}

func playerEloForGame(p *game.Player, gameName string) uint32 {
	if p == nil {
		return elo.ToUint32(elo.DefaultRating)
	}
	if rating, ok := p.Ratings[gameName]; ok {
		return rating
	}
	return elo.ToUint32(elo.DefaultRating)
}

// averageEloLocked is the table's average rating in gameName. An unnamed game has
// no ratings to average, so it reports the starting rating. Caller holds l.mu.
func (l *Lobby) averageEloLocked(gameName string) uint32 {
	if gameName == "" {
		return elo.ToUint32(elo.DefaultRating)
	}
	totalElo := playerEloForGame(l.leader, gameName)
	count := uint32(1)
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

func (l *Lobby) addGuest(p *game.Player) error {
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
	l.broadcastLocked(Event{Type: EventPlayersUpdated})
	return nil
}

func (l *Lobby) hasPlayerLocked(p *game.Player) bool {
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
	if !l.allReadyLocked() {
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

	players := slices.Concat([]*game.Player{l.leader}, l.guests)
	engine := game.NewEngine(rules, players, rules.InitialDeck())

	if err := engine.Start(); err != nil {
		return nil, fmt.Errorf("failed to start game engine: %w", err)
	}

	if l.manager != nil && l.manager.matchRepo != nil {
		// This watcher is the only thing that persists the result, so a failure to
		// subscribe costs the players their match history and Elo. The engine sizes
		// its broadcaster at len(players)+8 precisely so this cannot happen; if it
		// ever does, the match is still playable and the loss is recorded loudly.
		ch, err := engine.Broadcaster().Subscribe()
		if err != nil {
			slog.Error("cannot watch game for completion; result will not be persisted",
				"error", err, "lobby", l.code, "game", l.options.cardGame.Name)
		} else {
			go func() {
				defer engine.Broadcaster().Unsubscribe(ch)
				l.handleBroadcasterEvents(ch, engine)
			}()
		}
	}

	l.setStateLocked(InGame)
	l.activeEngine = engine
	clear(l.ready)

	observability.GamesStartedTotal.Add(1)

	l.broadcastLocked(Event{
		Type:    EventGameStarted,
		Payload: engine,
	})

	return engine, nil
}

func (l *Lobby) handleBroadcasterEvents(ch <-chan game.Event, engine *game.Engine) {
	for event := range ch {
		if event.Type == game.EventGameEnded {
			l.finalizeFinishedGame(engine)
			// The table has to reopen as soon as the match is over, not the next time
			// somebody presses ready: until it does the lobby is still InGame, and a
			// leader who inherited it when everyone else left finds that no setting
			// on the screen can be changed.
			l.releaseFinishedGame()
			return
		}
	}
}

// releaseFinishedGameLocked returns a lobby whose match is over to Waiting so the
// table can be reconfigured and played again, and hands back the engine to close.
// Closing is the caller's job because both callers already hold l.mu, and this is a
// no-op unless there really is a finished game to release. Caller holds l.mu.
func (l *Lobby) releaseFinishedGameLocked() *game.Engine {
	if l.state != InGame || l.activeEngine == nil || !l.activeEngine.IsFinished() {
		return nil
	}
	finished := l.activeEngine
	l.setStateLocked(Waiting)
	l.activeEngine = nil
	clear(l.ready)
	return finished
}

// releaseFinishedGame is releaseFinishedGameLocked for a caller holding no lock. It
// announces the reopened table so every lobby view re-reads the settings it may now
// change.
func (l *Lobby) releaseFinishedGame() {
	l.mu.Lock()
	finished := l.releaseFinishedGameLocked()
	bc := l.broadcaster
	l.mu.Unlock()

	if finished == nil {
		return
	}
	finished.Close()
	if bc != nil {
		bc.Broadcast(Event{Type: EventPlayersUpdated})
	}
}

// finalizeFinishedGame persists the result of a game that just ended. It is its
// own function so the finalizing counter and the write context are released by
// defer: leaking either would leave shutdown waiting on a write that is over.
func (l *Lobby) finalizeFinishedGame(engine *game.Engine) {
	standings, places := engine.StandingsWithPlaces()
	userIDs := make([]uint, 0, len(standings))
	for _, p := range standings {
		if p == nil || p.UserID == 0 {
			slog.Error("standing player missing database user; skipping ranked finalize")
			return
		}
		userIDs = append(userIDs, p.UserID)
	}

	// Guarded before any use of l.manager below, not after.
	if l.manager == nil || l.manager.matchRepo == nil {
		return
	}

	l.mu.RLock()
	isRanked := l.options.isRanked
	gameName := ""
	if l.options.cardGame != nil {
		gameName = l.options.cardGame.Name
	}
	parentCtx := l.manager.shutdownCtx()
	l.mu.RUnlock()

	if gameName == "" {
		return
	}

	if !l.manager.registerFinalizer() {
		return
	}
	defer l.manager.finalizing.Done()
	ctx, cancel := context.WithTimeout(parentCtx, rankedFinalizeTimeout)
	defer cancel()

	if err := l.recordFinishedMatch(ctx, gameName, userIDs, places, isRanked); err != nil {
		slog.Error("failed to record finished match", "error", err, "game", gameName, "ranked", isRanked)
	}
}

// recordFinishedMatch writes match history for every finished game. Only a ranked
// lobby also moves Elo; a casual one still belongs in the players' history.
func (l *Lobby) recordFinishedMatch(
	ctx context.Context, gameName string, userIDs []uint, places []int, isRanked bool,
) error {
	repo := l.manager.matchRepo
	if isRanked {
		if err := repo.FinalizeRankedMatch(ctx, gameName, userIDs, places); err != nil {
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
