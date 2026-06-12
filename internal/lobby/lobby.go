package lobby

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"terminalcard/internal/broadcaster"
	"terminalcard/internal/db"
	"terminalcard/internal/game"
	"terminalcard/internal/player"
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

func setupDefaultOptions() *options {
	return &options{
		maxPlayers: 4,
		isPrivate:  true,
		isRanked:   true,
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

func (l *Lobby) ToggleReady(p *player.Player, registry *game.Registry) error {
	l.mu.Lock()

	if l.state == InGame {
		if l.activeEngine != nil && l.activeEngine.IsFinished() {
			l.state = Waiting
			l.activeEngine = nil
			clear(l.ready)
		} else {
			l.mu.Unlock()
			return errors.New("game is already in progress")
		}
	}

	if !l.hasPlayerNoLock(p) {
		l.mu.Unlock()
		return errors.New("player not in lobby")
	}

	l.ready[p.ID] = !l.ready[p.ID]

	allReady := true
	if !l.ready[l.leader.ID] {
		allReady = false
	} else {
		for _, g := range l.guests {
			if !l.ready[g.ID] {
				allReady = false
				break
			}
		}
	}

	l.mu.Unlock()

	if allReady {
		_, err := l.startGame(registry)
		return err
	}

	if l.broadcaster != nil {
		l.broadcaster.Broadcast(Event{Type: "PLAYERS_UPDATED"})
	}

	return nil
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

func (l *Lobby) IsReady(p *player.Player) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.ready[p.ID]
}

func (l *Lobby) averageElo() uint32 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	var totalElo uint32
	var count uint32
	// check leader
	found := false
	for _, r := range l.leader.DatabaseUser.Rankings {
		if r.Game.Name == l.options.cardGame.Name {
			totalElo += r.Elo
			count++
			found = true
			break
		}
	}
	if !found {
		totalElo += 1500
		count++
	}
	for _, g := range l.guests {
		found := false
		for _, r := range g.DatabaseUser.Rankings {
			if r.Game.Name == l.options.cardGame.Name {
				totalElo += r.Elo
				count++
				found = true
				break
			}
		}
		if !found {
			totalElo += 1500
			count++
		}
	}
	return totalElo / count
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
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

func (l *Lobby) hasPlayerNoLock(p *player.Player) bool {
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

func (l *Lobby) startGame(registry *game.Registry) (*game.Engine, error) {
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

	if l.manager.matchRepo != nil {
		ch := engine.Broadcaster().Subscribe()
		go l.handleBroadcasterEvents(ch, engine)
	}

	l.state = InGame
	l.activeEngine = engine
	clear(l.ready)

	l.broadcaster.Broadcast(Event{
		Type:    "GAME_STARTED",
		Payload: engine,
	})

	return engine, nil
}

func (l *Lobby) handleBroadcasterEvents(ch <-chan game.Event, engine *game.Engine) {
	for event := range ch {
		if event.Type == game.EventGameEnded {
			standings := engine.Standings()
			userIDs := make([]uint, len(standings))
			for i, p := range standings {
				userIDs[i] = p.DatabaseUser.ID
			}
			if l.options.isRanked && l.options.cardGame != nil {
				deltas, err := l.manager.matchRepo.UpdateRankings(context.Background(), l.options.cardGame.ID, userIDs)
				if err != nil {
					slog.Error("failed to update game rankings", "error", err)
				} else {
					if err := l.manager.matchRepo.RecordMatch(context.Background(), l.options.cardGame.ID, userIDs, deltas); err != nil {
						slog.Error("failed to record match history", "error", err)
					}
				}
			}
			break
		}
	}
}
