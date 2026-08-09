package game

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/views"

	tea "charm.land/bubbletea/v2"
)

// EventMsg carries an engine event into the bubbletea loop. Views match on it in
// Update and call Session.Listen again to wait for the next one.
type EventMsg game.Event

// Session is the plumbing every game view repeats: the engine binding, the event
// subscription, the cached base state, and the hand cursor. Views embed it so that
// leaving a table, losing a seat to the idle timer, and moving the cursor behave the
// same everywhere, and so each new game starts with only its own rules to render.
//
// Any view embedding a Session must call Close on navigation (router.Closer):
// skipping it parks a listener goroutine and burns a subscriber slot on the engine.
type Session struct {
	Global router.GlobalContext
	Bound  *game.BoundEngine
	Events <-chan game.Event

	Base BaseState
	// Selected indexes Base.Hand and is clamped by SyncBase as the hand shrinks.
	Selected int
}

// NewSession binds engine to the session player and subscribes to its events. The
// returned error is for display, not a failure: a view without a subscription still
// renders, it just will not update, so it tells the player to rejoin.
func NewSession(global router.GlobalContext, engine *game.Engine, gameName string) (Session, error) {
	playerID := views.SessionPlayerID(global)

	s := Session{Global: global, Bound: game.Bind(engine, playerID)}
	if s.Bound == nil {
		return s, nil
	}

	ch, err := s.Bound.Subscribe()
	if err != nil {
		slog.Error("game view could not subscribe to engine events",
			"error", err, "game", gameName, "player_id", playerID)
		return s, fmt.Errorf("live table updates unavailable, leave and rejoin: %w", err)
	}
	s.Events = ch
	return s, nil
}

// Listen waits for the next engine event.
func (s *Session) Listen() tea.Cmd {
	return views.ListenOn(s.Events, func(ev game.Event) tea.Msg { return EventMsg(ev) })
}

// IdleRemoved reports whether ev says this session's own player lost their seat for
// idling. Everyone else's removal is just another state change.
func (s *Session) IdleRemoved(ev game.Event) bool {
	return ev.Type == game.EventPlayerIdle && s.Bound != nil && ev.PlayerID == s.Bound.PlayerID()
}

// SyncBase refreshes the cached engine state and keeps the cursor inside the hand.
func (s *Session) SyncBase() {
	s.Base = SyncBaseState(s.Bound)
	if s.Selected >= len(s.Base.Hand) {
		s.Selected = max(len(s.Base.Hand)-1, 0)
	}
}

// WithHiddenState reads the per-game slice of engine state under the engine lock.
// What it hands back is not redacted for this player, so anything derived from it
// that reaches the screen has to be filtered by the caller.
func (s *Session) WithHiddenState(fn func(extra any)) {
	if s.Bound != nil {
		s.Bound.WithHiddenState(fn)
	}
}

var errNotSeated = errors.New("you are not seated at this table")

// Submit sends action as this session's player. The error is rendered to the player
// as-is, which is why it is passed through untouched.
//
//nolint:wrapcheck // engine errors are player-facing prose; a wrap adds call-site noise to the UI line.
func (s *Session) Submit(action game.Action) error {
	if s.Bound == nil {
		return errNotSeated
	}
	return s.Bound.Submit(action)
}

// SelectedCard is the card under the cursor.
func (s *Session) SelectedCard() (deck.Card, bool) {
	if s.Selected < 0 || s.Selected >= len(s.Base.Hand) {
		return deck.Card{}, false
	}
	return s.Base.Hand[s.Selected], true
}

// MoveCursor steps the hand cursor, stopping at either end.
func (s *Session) MoveCursor(delta int) {
	s.Selected = min(max(s.Selected+delta, 0), max(len(s.Base.Hand)-1, 0))
}

// SelectDigit moves the cursor to the card a number key names. Keys past the end of
// the hand are ignored, which is why only the first ten cards are reachable this way.
func (s *Session) SelectDigit(key string) {
	if len(key) != 1 || key[0] < '0' || key[0] > '9' {
		return
	}
	if idx := int(key[0] - '0'); idx < len(s.Base.Hand) {
		s.Selected = idx
	}
}

// Unsubscribe releases the engine subscription. Safe to call more than once.
func (s *Session) Unsubscribe() {
	if s.Bound != nil && s.Events != nil {
		s.Bound.Unsubscribe(s.Events)
		s.Events = nil
	}
}

// Close implements router.Closer.
func (s *Session) Close() {
	s.Unsubscribe()
}

// Leave navigates away from the table: back to the lobby once the game has finished,
// otherwise out of the lobby entirely, since leaving mid-game forfeits the seat.
func (s *Session) Leave() tea.Cmd {
	p := views.SessionPlayer(s.Global)
	finished := s.Base.Phase == game.Finished

	if p != nil && !finished {
		s.Global.LobbyManager.LeaveLobby(p)
	}
	s.Unsubscribe()

	if p == nil || !finished {
		return navigateTo(router.RouteHome, nil)
	}
	return navigateTo(router.RouteLobby, s.Global.LobbyManager.FindLobbyByPlayer(p))
}

func navigateTo(route string, context any) tea.Cmd {
	return func() tea.Msg {
		return router.ChangeViewMsg{ViewName: route, Context: context}
	}
}
