package tui

import (
	"context"

	"github.com/Pieczasz/terminal-card/internal/catalog"
	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/views"
	"github.com/Pieczasz/terminal-card/internal/tui/views/home"
	"github.com/Pieczasz/terminal-card/internal/tui/views/leaderboard"
	"github.com/Pieczasz/terminal-card/internal/tui/views/lobby"
	"github.com/Pieczasz/terminal-card/internal/tui/views/profile"

	internallobby "github.com/Pieczasz/terminal-card/internal/lobby"

	tea "charm.land/bubbletea/v2"
)

type ModelDependencies struct {
	SessionCtx   context.Context
	User         db.User
	UserRepo     db.UserRepository
	MatchRepo    db.MatchRepository
	LobbyManager *internallobby.Manager
	GameRegistry *game.Registry
}

// Model builds the session's root model. It returns the router itself rather than a
// tea.Model: the ssh layer has to Close it when the session ends, and an interface
// value would hide the one method that releases the active view's subscription.
func Model(deps ModelDependencies) *router.Router {
	global := router.GlobalContext{
		User:           &deps.User,
		UserRepository: deps.UserRepo,
		LobbyManager:   deps.LobbyManager,
		GameRegistry:   deps.GameRegistry,
		SessionCtx:     deps.SessionCtx,
	}

	r := router.New(global)

	r.Register(router.RouteHome, func(g router.GlobalContext, _ any) tea.Model {
		return home.New(g)
	})

	r.Register(router.RouteProfile, func(g router.GlobalContext, _ any) tea.Model {
		return profile.New(g)
	})

	r.Register(router.RouteLeaderboard, func(g router.GlobalContext, _ any) tea.Model {
		return leaderboard.New(g)
	})

	r.Register(router.RouteLobbyCreate, func(g router.GlobalContext, _ any) tea.Model {
		p := views.SessionPlayer(g)
		if l := g.LobbyManager.FindLobbyByPlayer(p); l != nil {
			return lobby.New(g, l)
		}
		return lobby.NewCreate(g)
	})

	r.Register(router.RouteLobbyJoin, func(g router.GlobalContext, _ any) tea.Model {
		p := views.SessionPlayer(g)
		if l := g.LobbyManager.FindLobbyByPlayer(p); l != nil {
			return lobby.New(g, l)
		}
		return lobby.NewJoin(g)
	})

	r.Register(router.RouteLobby, func(g router.GlobalContext, ctx any) tea.Model {
		l, ok := ctx.(*internallobby.Lobby)
		if !ok {
			return home.New(g)
		}
		return lobby.New(g, l)
	})

	registerGameViews(r)

	// No Goto here: the router builds its first view in Init, so that view's Init
	// runs exactly once. See Router.Init.
	return r
}

func registerGameViews(r *router.Router) {
	for _, e := range catalog.All {
		r.Register(router.GameRoute(e.Slug), func(g router.GlobalContext, ctx any) tea.Model {
			engine, ok := ctx.(*game.Engine)
			if !ok {
				return home.New(g)
			}
			return e.View(g, engine)
		})
	}
}
