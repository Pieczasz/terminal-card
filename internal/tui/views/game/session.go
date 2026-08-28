package game

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/observability"
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
	// gameName labels the rejected-action metric. It is the display name the
	// registry and db.Game.Name use, so the counter lines up with everything else
	// recorded per game.
	gameName string

	Base BaseState
	// Selected indexes Base.Hand and is clamped by SyncBase as the hand shrinks.
	Selected int
}

// NewSession binds engine to the session player and subscribes to its events. The
// returned error is for display, not a failure: a view without a subscription still
// renders, it just will not update, so it tells the player to rejoin.
func NewSession(global router.GlobalContext, engine *game.Engine, gameName string) (Session, error) {
	playerID := views.SessionPlayerID(global)

	s := Session{Global: global, Bound: game.Bind(engine, playerID), gameName: gameName}
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

// Sync refreshes the cached engine state and keeps the cursor inside the hand.
//
// extra, when set, reads the per-game slice of engine state in the same lock hold, so
// a view's own fields describe the same moment as its base state. What extra is handed
// is not redacted for this player, so anything derived from it that reaches the screen
// has to be filtered by the caller.
func (s *Session) Sync(extra func(extra any)) {
	s.Base = SyncBaseState(s.Bound, extra)
	if s.Selected >= len(s.Base.Hand) {
		s.Selected = max(len(s.Base.Hand)-1, 0)
	}
}

// HandleFrame runs the part of Update that is the same at every table: the common
// window, theme and quit keys, the engine's event feed, and the turn-clock tick. sync
// is the view's own state refresh; onEvent, when set, runs before an event-driven
// sync. It reports whether the message was consumed, so a view falls through to its
// own key bindings.
func (s *Session) HandleFrame(msg tea.Msg, sync func(), onEvent func()) (tea.Cmd, bool) {
	if handled, cmd := views.HandleCommonMsg(msg, &s.Global); handled {
		return cmd, true
	}

	switch msg := msg.(type) {
	case EventMsg:
		if s.IdleRemoved(game.Event(msg)) {
			// The engine took this seat for repeated missed turns. Quitting ends the
			// bubbletea program, which is what tears the ssh session down and runs the
			// ordinary leave path.
			return tea.Quit, true
		}
		wasPlaying := s.Base.Phase == game.Playing
		if onEvent != nil {
			onEvent()
		}
		sync()
		cmds := []tea.Cmd{s.Listen()}
		// Re-arm the countdown when the table starts playing. A view built while the
		// lobby was still waiting stops its own tick chain on the first tick - the
		// clock would then never run for that player for the whole match.
		if !wasPlaying && s.Base.Phase == game.Playing {
			cmds = append(cmds, ClockTickFor(s.Base.TurnRemaining, s.Base.MyTurn))
		}
		return tea.Batch(cmds...), true
	case ClockTickMsg:
		sync()
		if s.Base.Phase != game.Playing {
			return nil, true
		}
		return ClockTickFor(s.Base.TurnRemaining, s.Base.MyTurn), true
	}

	return nil, false
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
	err := s.Bound.Submit(action)
	if err != nil {
		// Background, not the session context: a rejection is worth counting even
		// when it is the disconnect itself that caused it.
		observability.ActionRejected(context.Background(), s.gameName)
	}
	return err
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
