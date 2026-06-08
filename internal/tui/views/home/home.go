package home

import (
	"fmt"
	"terminalcard/internal/tui/router"
	"terminalcard/internal/tui/styles"
	"terminalcard/internal/tui/views/common"

	tea "github.com/charmbracelet/bubbletea"
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
	if handled, cmd := common.HandleCommonMsg(msg, &m.global); handled {
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "n":
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "lobby_create"} }
		case "f":
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "lobby_join"} }
		case "p":
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "profile"} }
		}
	}
	return m, nil
}

func (m model) View() string {
	innerWidth := styles.GetInnerWidth(m.global.Width)

	titleFig := styles.RenderFigureASCII("Terminal Cards", innerWidth)
	titleText := styles.Title.Render(titleFig)

	welcomeUser := fmt.Sprintf("Welcome %s", m.global.User.Username)
	welcomeFig := styles.RenderFigureASCII(welcomeUser, innerWidth)
	welcomeText := styles.Welcome.Render(welcomeFig)

	homePageActions := styles.RenderActionFooter(styles.GlobalActions)

	return common.RenderCenteredLayout(m.global.Width, m.global.Height, titleText, welcomeText, homePageActions)
}
