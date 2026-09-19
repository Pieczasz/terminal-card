package home

import (
	"fmt"

	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"
	"github.com/Pieczasz/terminal-card/internal/tui/views"

	tea "charm.land/bubbletea/v2"
)

type model struct {
	global router.GlobalContext
}

func New(global router.GlobalContext) tea.Model {
	return model{global: global}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if handled, cmd := views.HandleCommonMsg(msg, &m.global); handled {
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "n":
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: router.RouteLobbyCreate} }
		case "f":
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: router.RouteLobbyJoin} }
		case "p":
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: router.RouteProfile} }
		case "t":
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: router.RouteLeaderboard} }
		}
	}
	return m, nil
}

func (m model) View() tea.View {
	welcomeName := "Player"
	if m.global.User != nil {
		welcomeName = m.global.User.Username
	}

	return tea.NewView(views.RenderScreen(m.global, "Terminal Cards", nil, func(height int) string {
		// The welcome banner is the whole content, so it may use every line it is handed.
		welcomeFig := styles.RenderFigureASCII(
			fmt.Sprintf("Welcome %s", welcomeName), styles.InnerWidth(m.global.Width), height)
		return m.global.Theme.Welcome.Render(welcomeFig)
	}))
}
