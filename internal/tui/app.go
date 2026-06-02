package tui

import (
	"client/internal/db"
	"client/internal/game"
	internal_lobby "client/internal/lobby"
	"client/internal/player"
	"client/internal/tui/router"
	gameview "client/internal/tui/views/game"
	"client/internal/tui/views/home"
	"client/internal/tui/views/lobby"
	"client/internal/tui/views/profile"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

func Model(user db.User, queries *db.Queries, lobbyManager *internal_lobby.Manager, gameRegistry *game.Registry) tea.Model {
	global := router.GlobalContext{
		User:         user,
		Queries:      queries,
		LobbyManager: lobbyManager,
		GameRegistry: gameRegistry,
	}

	r := router.New(global)

	r.Register("home", func(g router.GlobalContext, _ any) tea.Model {
		return home.New(g)
	})

	r.Register("profile", func(g router.GlobalContext, _ any) tea.Model {
		return profile.New(g)
	})

	r.Register("lobby_create", func(g router.GlobalContext, _ any) tea.Model {
		p := &player.Player{Id: fmt.Sprint(g.User.ID), DatabaseUser: &g.User}
		if l := g.LobbyManager.FindLobbyByPlayer(p); l != nil {
			return lobby.New(g, l)
		}
		return lobby.NewCreate(g)
	})

	r.Register("lobby_join", func(g router.GlobalContext, _ any) tea.Model {
		p := &player.Player{Id: fmt.Sprint(g.User.ID), DatabaseUser: &g.User}
		if l := g.LobbyManager.FindLobbyByPlayer(p); l != nil {
			return lobby.New(g, l)
		}
		return lobby.NewJoin(g)
	})

	r.Register("lobby", func(g router.GlobalContext, ctx any) tea.Model {
		// ctx should be the *lobby.Lobby
		l, _ := ctx.(*internal_lobby.Lobby)
		return lobby.New(g, l)
	})

	r.Register("game", func(g router.GlobalContext, ctx any) tea.Model {
		// ctx should be the *game.Engine
		e, _ := ctx.(*game.Engine)
		return gameview.New(g, e)
	})

	r.Goto("home", nil)

	return r
}
