package tui

import (
	"context"
	"fmt"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/player"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/views/game/crazyeight"
	"github.com/Pieczasz/terminal-card/internal/tui/views/game/poker"
	"github.com/Pieczasz/terminal-card/internal/tui/views/home"
	"github.com/Pieczasz/terminal-card/internal/tui/views/leaderboard"
	"github.com/Pieczasz/terminal-card/internal/tui/views/lobby"
	"github.com/Pieczasz/terminal-card/internal/tui/views/profile"

	internallobby "github.com/Pieczasz/terminal-card/internal/lobby"

	tea "charm.land/bubbletea/v2"
)

// ViewFactory builds a TUI model for a started game engine.
type ViewFactory func(g router.GlobalContext, e *game.Engine) tea.Model

// gameViews maps module slug → TUI factory. Add an entry when registering a new Module.
var gameViews = map[string]ViewFactory{
	"crazy_eights": crazyeight.New,
	"poker":        poker.New,
}

func Model(sessionCtx context.Context, user db.User, userRepo db.UserRepository, matchRepo db.MatchRepository, lobbyManager *internallobby.Manager, gameRegistry *game.Registry) tea.Model {
	global := router.GlobalContext{
		User:            &user,
		UserRepository:  userRepo,
		MatchRepository: matchRepo,
		LobbyManager:    lobbyManager,
		GameRegistry:    gameRegistry,
		SessionCtx:      sessionCtx,
	}

	r := router.New(global)

	r.Register("home", func(g router.GlobalContext, _ any) tea.Model {
		return home.New(g)
	})

	r.Register("profile", func(g router.GlobalContext, _ any) tea.Model {
		return profile.New(g)
	})

	r.Register("leaderboard", func(g router.GlobalContext, _ any) tea.Model {
		return leaderboard.New(g)
	})

	r.Register("lobby_create", func(g router.GlobalContext, _ any) tea.Model {
		p := &player.Player{ID: fmt.Sprint(g.User.ID), DatabaseUser: g.User}
		if l := g.LobbyManager.FindLobbyByPlayer(p); l != nil {
			return lobby.New(g, l)
		}
		return lobby.NewCreate(g)
	})

	r.Register("lobby_join", func(g router.GlobalContext, _ any) tea.Model {
		p := &player.Player{ID: fmt.Sprint(g.User.ID), DatabaseUser: g.User}
		if l := g.LobbyManager.FindLobbyByPlayer(p); l != nil {
			return lobby.New(g, l)
		}
		return lobby.NewJoin(g)
	})

	r.Register("lobby", func(g router.GlobalContext, ctx any) tea.Model {
		l, ok := ctx.(*internallobby.Lobby)
		if !ok {
			return home.New(g)
		}
		return lobby.New(g, l)
	})

	registerGameViews(r, gameRegistry)

	r.Goto("home", nil)

	return r
}

func registerGameViews(r *router.Router, gameRegistry *game.Registry) {
	for _, name := range gameRegistry.GameNames() {
		mod, ok := gameRegistry.Module(name)
		if !ok {
			continue
		}
		viewFactory, ok := gameViews[mod.Slug]
		if !ok {
			continue
		}
		route := mod.RouteName()
		vf := viewFactory
		r.Register(route, func(g router.GlobalContext, ctx any) tea.Model {
			e, ok := ctx.(*game.Engine)
			if !ok {
				return home.New(g)
			}
			return vf(g, e)
		})
	}
}
