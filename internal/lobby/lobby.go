package lobby

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

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
	// createdAt and startedAt are what turn "a game ended" into the two durations
	// worth watching: how long a table waited for players, and how long a hand ran.
	createdAt time.Time
	startedAt time.Time
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

	if _, err := l.startGameLocked(registry); err != nil {
		// The ready flip is already committed, so the other clients have to see it even
		// though the start failed; otherwise their rosters disagree with the server
		// until something else happens to broadcast.
		l.broadcastLocked(Event{Type: EventPlayersUpdated})
		return err
	}
	return nil
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

// ActiveGame returns the running engine, or nil while the lobby is not mid-game.
// A reconnecting player's view uses it to land back at the table.
func (l *Lobby) ActiveGame() *game.Engine {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.state != InGame {
		return nil
	}
	return l.activeEngine
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

	l.watchGameLocked(engine)

	l.setStateLocked(InGame)
	l.activeEngine = engine
	l.startedAt = time.Now()
	clear(l.ready)

	observability.GameStarted(context.Background(), l.options.cardGame.Name, l.options.isRanked)
	if !l.createdAt.IsZero() {
		observability.LobbyStarted(context.Background(), l.options.cardGame.Name, time.Since(l.createdAt))
	}

	l.broadcastLocked(Event{
		Type:    EventGameStarted,
		Payload: engine,
	})

	return engine, nil
}

// watchGameLocked starts the goroutine that persists the result and counts the
// engine's own events. It is the only thing that persists a match, so a failure to
// subscribe costs the players their history and Elo. The engine sizes its
// broadcaster at len(players)+8 precisely so this cannot happen; if it ever does,
// the match is still playable and the loss is recorded loudly. Caller holds l.mu.
func (l *Lobby) watchGameLocked(engine *game.Engine) {
	if l.manager == nil || l.manager.matchRepo == nil {
		return
	}
	ch, err := engine.Broadcaster().Subscribe()
	if err != nil {
		observability.SubscribeFailure(l.manager.shutdownCtx(), "game")
		slog.ErrorContext(l.manager.shutdownCtx(),
			"cannot watch game for completion; result will not be persisted",
			"error", err, "lobby", l.code, "game", l.options.cardGame.Name)
		return
	}
	go func() {
		defer engine.Broadcaster().Unsubscribe(ch)
		l.handleBroadcasterEvents(ch, engine)
	}()
}

func (l *Lobby) handleBroadcasterEvents(ch <-chan game.Event, engine *game.Engine) {
	ctx := l.manager.shutdownCtx()
	gameName := l.GameName()
	defer func() {
		if n := engine.Broadcaster().Dropped(); n > 0 {
			observability.BroadcastDropped(ctx, "game", n)
		}
	}()

	for event := range ch {
		switch event.Type {
		case game.EventTurnTimedOut:
			observability.TurnTimedOut(ctx, gameName)
		case game.EventPlayerIdle:
			observability.PlayerIdleRemoved(ctx, gameName)
			// The engine took the seat, so the lobby roster follows: without this a
			// disconnected player kicked for idling would reconnect into a lobby
			// whose game no longer has them. Equal falls back to ID for a zero
			// UserID, so the stub player matches the roster entry.
			l.manager.LeaveLobby(&game.Player{ID: event.PlayerID})
		case game.EventGameEnded:
			l.finalizeFinishedGame(engine, event.Reason)
			// The table has to reopen as soon as the match is over, not the next time
			// somebody presses ready: until it does the lobby is still InGame, and a
			// leader who inherited it when everyone else left finds that no setting
			// on the screen can be changed.
			l.releaseFinishedGame()
			return
		}
	}

	// The feed ended without a terminal event. That is not proof the match did not
	// finish: the broadcaster is latest-wins and can drop EventGameEnded, and
	// RemoveLobby closes the feed out from under this goroutine. A finished engine
	// still holds a result the players expect in their history.
	if engine.IsFinished() {
		l.finalizeFinishedGame(engine, game.EndReasonUnknown)
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

// finalizeFinishedGame persists the result of a game that just ended.
//
// registerFinalizer is the first thing it does, before reading standings or lobby
// settings: every statement between observing the end of the game and that call is a
// window in which shutdown can begin, and a refusal after the window means a
// finished match is dropped with nothing left for WaitForFinalizers to wait on.
func (l *Lobby) finalizeFinishedGame(engine *game.Engine, reason game.EndReason) {
	// Guarded before any use of l.manager below, not after.
	if l.manager == nil || l.manager.matchRepo == nil {
		return
	}
	registered := l.manager.registerFinalizer()
	parentCtx := l.manager.shutdownCtx()

	l.mu.RLock()
	isRanked := l.options.isRanked
	startedAt := l.startedAt
	gameName := ""
	if l.options.cardGame != nil {
		gameName = l.options.cardGame.Name
	}
	l.mu.RUnlock()

	if !registered {
		slog.ErrorContext(parentCtx, "finished match dropped; shutdown stopped new finalizers",
			"lobby", l.code, "game", gameName, "ranked", isRanked)
		observability.MatchFinalize(parentCtx, "dropped", isRanked)
		return
	}
	defer l.manager.finalizing.Done()

	if !startedAt.IsZero() {
		observability.GameFinished(parentCtx, gameName, isRanked, endReasonLabel(reason), time.Since(startedAt))
	}
	if gameName == "" {
		return
	}

	ctx, cancel := context.WithTimeout(parentCtx, rankedFinalizeTimeout)
	defer cancel()
	l.persistFinishedMatch(ctx, engine, reason, gameName, isRanked)
}

// endReasonLabel keeps the metric attribute a small closed set.
func endReasonLabel(reason game.EndReason) string {
	switch reason {
	case game.EndReasonWin:
		return "win"
	case game.EndReasonRulesError:
		return "rules_error"
	case game.EndReasonForfeit:
		return "forfeit"
	case game.EndReasonAbandoned:
		return "abandoned"
	case game.EndReasonUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// persistFinishedMatch writes the standings of a finished game. Both bail-outs are
// logged with the lobby attached: a match that never reaches the database is
// invisible to everyone except this line.
func (l *Lobby) persistFinishedMatch(
	ctx context.Context, engine *game.Engine, reason game.EndReason, gameName string, isRanked bool,
) {
	standings, places := engine.StandingsWithPlaces()
	// A table every seat walked out of has no result worth a row.
	if reason == game.EndReasonAbandoned && len(standings) == 0 {
		return
	}

	userIDs := make([]uint, 0, len(standings))
	for i, p := range standings {
		if p == nil || p.UserID == 0 {
			slog.ErrorContext(ctx, "standing player has no database user; match not recorded",
				"lobby", l.code, "game", gameName, "ranked", isRanked, "player_index", i)
			observability.MatchFinalize(ctx, "dropped", isRanked)
			return
		}
		userIDs = append(userIDs, p.UserID)
	}

	// A match the deploy interrupted has no honest winner: SSH teardown order, not
	// play, decided who was left holding cards. The history row is still wanted, the
	// Elo write is not.
	rated := isRanked && !l.manager.isShuttingDown()
	if isRanked && !rated {
		slog.WarnContext(ctx, "server is shutting down; recording the ranked match without Elo",
			"lobby", l.code, "game", gameName)
	}

	if err := l.recordFinishedMatch(ctx, gameName, userIDs, places, rated); err != nil {
		slog.ErrorContext(ctx, "failed to record finished match",
			"error", err, "lobby", l.code, "game", gameName, "ranked", rated)
		observability.MatchFinalize(ctx, "error", isRanked)
		return
	}
	observability.MatchFinalize(ctx, "ok", isRanked)
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
	// A casual result is history only, it must not move ratings.
	if err := repo.RecordCasualMatch(ctx, gameName, userIDs); err != nil {
		return fmt.Errorf("record casual match: %w", err)
	}
	return nil
}
