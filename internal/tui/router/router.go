package router

import (
	"client/internal/db"
	"client/internal/game"
	"client/internal/lobby"

	tea "github.com/charmbracelet/bubbletea"
)

type ChangeViewMsg struct {
	ViewName string
	Context  any
}

type GlobalContext struct {
	User         *db.User
	Queries      *db.Queries
	LobbyManager *lobby.Manager
	GameRegistry *game.Registry
	Width        int
	Height       int
}

type Router struct {
	Global    GlobalContext
	views     map[string]func(GlobalContext, any) tea.Model
	active    tea.Model
	activeKey string
}

func New(global GlobalContext) *Router {
	r := &Router{
		Global: global,
		views:  make(map[string]func(GlobalContext, any) tea.Model),
	}
	return r
}

func (r *Router) Register(name string, factory func(GlobalContext, any) tea.Model) {
	r.views[name] = factory
}

func (r *Router) Goto(name string, context any) tea.Cmd {
	factory, ok := r.views[name]
	if !ok {
		return nil
	}
	r.active = factory(r.Global, context)
	r.activeKey = name
	return r.active.Init()
}

func (r *Router) Init() tea.Cmd {
	if r.active != nil {
		return r.active.Init()
	}
	return nil
}

func (r *Router) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		r.Global.Width = msg.Width
		r.Global.Height = msg.Height
	case ChangeViewMsg:
		cmd := r.Goto(msg.ViewName, msg.Context)
		return r, cmd
	}

	if r.active != nil {
		var cmd tea.Cmd
		r.active, cmd = r.active.Update(msg)
		return r, cmd
	}

	return r, nil
}

func (r *Router) View() string {
	if r.active != nil {
		return r.active.View()
	}
	return "No active view"
}
