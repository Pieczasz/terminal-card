package router

import (
	"context"
	"strings"
	"time"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/lobby"

	tea "charm.land/bubbletea/v2"
)

// Route keys for every registered view. Games are registered dynamically, so
// their routes are derived from the module slug via GameRoute.
const (
	RouteHome        = "home"
	RouteProfile     = "profile"
	RouteLeaderboard = "leaderboard"
	RouteLobby       = "lobby"
	RouteLobbyCreate = "lobby_create"
	RouteLobbyJoin   = "lobby_join"
	RouteGamePrefix  = "game_"
)

// GameRoute returns the route key for a game module slug.
func GameRoute(slug string) string {
	return RouteGamePrefix + slug
}

type ChangeViewMsg struct {
	ViewName string
	Context  any
}

type GlobalContext struct {
	User            *db.User
	UserRepository  db.UserRepository
	MatchRepository db.MatchRepository
	LobbyManager    *lobby.Manager
	GameRegistry    *game.Registry
	// SessionCtx is canceled when the SSH session ends.
	SessionCtx context.Context
	Width      int
	Height     int
}

// RequestContext returns the session context, or Background if unset.
func (g GlobalContext) RequestContext() context.Context {
	if g.SessionCtx != nil {
		return g.SessionCtx
	}
	return context.Background()
}

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(10*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

type Router struct {
	Global       GlobalContext
	views        map[string]func(GlobalContext, any) tea.Model
	active       tea.Model
	activeKey    string
	lastActivity time.Time
}

func New(global GlobalContext) *Router {
	r := &Router{
		Global:       global,
		views:        make(map[string]func(GlobalContext, any) tea.Model),
		lastActivity: time.Now(),
	}
	return r
}

func (r *Router) Register(name string, factory func(GlobalContext, any) tea.Model) {
	r.views[name] = factory
}

// Closer is implemented by views holding resources that outlive a render, such as
// an event-broadcaster subscription. The router releases them when the view is
// replaced or the session ends; without that, a player who disconnects mid-game
// leaves a listener goroutine parked and occupies a subscriber slot until the
// engine itself is closed.
type Closer interface {
	Close()
}

// closeActive releases the current view's resources if it holds any.
func (r *Router) closeActive() {
	if c, ok := r.active.(Closer); ok {
		c.Close()
	}
}

// Close releases the active view. Safe to call more than once, and safe to call
// on a router that never navigated anywhere.
func (r *Router) Close() {
	r.closeActive()
	r.active = nil
	r.activeKey = ""
}

func (r *Router) Goto(name string, context any) tea.Cmd {
	factory, ok := r.views[name]
	if !ok {
		return nil
	}
	r.closeActive()
	r.active = factory(r.Global, context)
	r.activeKey = name
	return r.active.Init()
}

func (r *Router) Init() tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, tick())
	if r.active != nil {
		if cmd := r.active.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

func (r *Router) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyPressMsg, tea.MouseMsg:
		r.lastActivity = time.Now()
	case tickMsg:
		if time.Since(r.lastActivity) > 5*time.Minute && !strings.HasPrefix(r.activeKey, RouteGamePrefix) {
			return r, tea.Quit
		}
		cmds = append(cmds, tick())
	case tea.WindowSizeMsg:
		r.Global.Width = msg.Width
		r.Global.Height = msg.Height
	case ChangeViewMsg:
		cmd := r.Goto(msg.ViewName, msg.Context)
		cmds = append(cmds, cmd)
		return r, tea.Batch(cmds...)
	}

	if r.active != nil {
		var cmd tea.Cmd
		r.active, cmd = r.active.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return r, tea.Batch(cmds...)
}

func (r *Router) View() tea.View {
	if r.active != nil {
		v := r.active.View()
		v.AltScreen = true
		return v
	}
	v := tea.NewView("No active view")
	v.AltScreen = true
	return v
}
