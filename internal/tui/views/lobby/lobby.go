package lobby

import (
	"client/internal/lobby"
	"client/internal/player"
	"client/internal/tui/router"
	"client/internal/tui/styles"
	"fmt"

	"github.com/common-nighthawk/go-figure"
	tea "github.com/charmbracelet/bubbletea"
	lg "github.com/charmbracelet/lipgloss"
)

type lobbyMsg lobby.LobbyEvent

type model struct {
	global       router.GlobalContext
	currentLobby *lobby.Lobby
	lobbyChan    <-chan lobby.LobbyEvent
}

func listenToLobbyBroadcaster(ch <-chan lobby.LobbyEvent) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return nil
		}
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return lobbyMsg(msg)
	}
}

// New returns a new lobby model. We pass the current active lobby through Context
func New(global router.GlobalContext, activeLobby *lobby.Lobby) tea.Model {
	ch := activeLobby.Broadcaster().Subscribe()
	return model{
		global:       global,
		currentLobby: activeLobby,
		lobbyChan:    ch,
	}
}

func (m model) Init() tea.Cmd {
	return listenToLobbyBroadcaster(m.lobbyChan)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.global.Width = msg.Width
		m.global.Height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "s":
			engine, err := m.currentLobby.StartGame(m.global.GameRegistry)
			if err == nil {
				return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "game", Context: engine} }
			}
		case "esc":
			p := &player.Player{Id: fmt.Sprint(m.global.User.ID), DatabaseUser: &m.global.User}
			if m.currentLobby != nil {
				if m.currentLobby.RemovePlayer(p) {
					m.global.LobbyManager.RemoveLobby(m.currentLobby.Code())
				}
			}
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "home"} }
		case "n":
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "lobby_create"} }
		case "f":
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "lobby_join"} }
		case "p":
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "profile"} }
		}
	case lobbyMsg:
		if msg.Type == "GAME_STARTED" {
			// Actually need the engine, which is awkward if we are not the leader.
			// To be solved fully later, for now we will assume the Context holds the engine.
			// Or the leader triggered the state transition.
		}
		return m, listenToLobbyBroadcaster(m.lobbyChan)
	}
	return m, nil
}

func (m model) View() string {
	if m.currentLobby == nil {
		return "No active lobby."
	}

	titleText := figure.NewFigure(fmt.Sprintf("Lobby Code: %s", m.currentLobby.Code()), "small", true).String()
	header := styles.Title.Render(titleText)
	
	footerActions := append([]string{"s - Start Game"}, styles.GlobalActions...)
	footer := lg.NewStyle().Render(styles.RenderActionFooter(footerActions))
	
	content := "Waiting for players..."
	
	return lg.Place(
		m.global.Width, m.global.Height,
		lg.Center, lg.Center,
		styles.RenderMainLayout(m.global.Width, m.global.Height, header, content, footer),
	)
}
