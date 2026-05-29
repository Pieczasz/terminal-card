package game

import (
	"client/internal/game"
	"client/internal/tui/router"
	"client/internal/tui/styles"

	tea "github.com/charmbracelet/bubbletea"
	lg "github.com/charmbracelet/lipgloss"
)

type gameMsg game.Event

type model struct {
	global   router.GlobalContext
	engine   *game.Engine
	gameChan <-chan game.Event
}

func listenToGameBroadcaster(ch <-chan game.Event) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return nil
		}
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return gameMsg(msg)
	}
}

func New(global router.GlobalContext, engine *game.Engine) tea.Model {
	//TODO:
	// Normally we would subscribe here if we haven't already.
	// For now we mock the subscription or assume it's passed.
	var ch <-chan game.Event
	// if engine != nil { ch = engine.Broadcaster().Subscribe() }
	return model{
		global:   global,
		engine:   engine,
		gameChan: ch,
	}
}

func (m model) Init() tea.Cmd {
	return listenToGameBroadcaster(m.gameChan)
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
		case "esc":
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "home"} }
		}
	case gameMsg:
		return m, listenToGameBroadcaster(m.gameChan)
	}
	return m, nil
}

func (m model) View() string {
	uiStack := lg.JoinVertical(lg.Center,
		styles.Title.Render("Active Game"),
		"(Game UI and board go here)",
		"\n\nesc - Leave Game",
	)

	return lg.Place(
		m.global.Width, m.global.Height,
		lg.Center, lg.Center,
		uiStack,
	)
}
