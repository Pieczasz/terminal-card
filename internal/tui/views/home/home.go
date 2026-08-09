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
	innerWidth := styles.InnerWidth(m.global.Width)

	titleFig := styles.RenderFigureASCII("Terminal Cards", innerWidth)
	titleText := m.global.Theme.Title.Render(titleFig)

	welcomeName := "Player"
	if m.global.User != nil {
		welcomeName = m.global.User.Username
	}
	welcomeUser := fmt.Sprintf("Welcome %s", welcomeName)
	welcomeFig := styles.RenderFigureASCII(welcomeUser, innerWidth)
	welcomeText := m.global.Theme.Welcome.Render(welcomeFig)

	homePageActions := m.global.Theme.RenderActionFooter(styles.GlobalActions)

	return tea.NewView(views.RenderCenteredLayout(m.global, titleText, welcomeText, homePageActions))
}
