package tui

import (
	"fmt"
	"terminalcard/internal/db"
	"terminalcard/internal/game"
	"terminalcard/internal/player"
	"terminalcard/internal/tui/router"
	"terminalcard/internal/tui/views/game/crazyeight"
	"terminalcard/internal/tui/views/home"
	"terminalcard/internal/tui/views/leaderboard"
	"terminalcard/internal/tui/views/lobby"
	"terminalcard/internal/tui/views/profile"

	internallobby "terminalcard/internal/lobby"

	tea "github.com/charmbracelet/bubbletea"
)

func Model(user db.User, userRepo db.UserRepository, matchRepo db.MatchRepository, lobbyManager *internallobby.Manager, gameRegistry *game.Registry) tea.Model {
	global := router.GlobalContext{
		User:            &user,
		UserRepository:  userRepo,
		MatchRepository: matchRepo,
		LobbyManager:    lobbyManager,
		GameRegistry:    gameRegistry,
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
		// ctx should be the *lobby.Lobby
		l, _ := ctx.(*internallobby.Lobby)
		return lobby.New(g, l)
	})

	r.Register("game_crazy_eights", func(g router.GlobalContext, ctx any) tea.Model {
		// ctx should be the *game.Engine
		e, _ := ctx.(*game.Engine)
		return crazyeight.New(g, e)
	})
	// TODO: after adding new games, register their view here
	r.Goto("home", nil)

	return r
}
