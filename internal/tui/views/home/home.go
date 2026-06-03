package home

import (
	"client/internal/tui/router"
	"client/internal/tui/styles"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	lg "github.com/charmbracelet/lipgloss"
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
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.global.Width = msg.Width
		m.global.Height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
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

	titleFig := styles.RenderFigureAscii("Terminal Cards", innerWidth)
	titleText := styles.Title.Render(titleFig)

	welcomeUser := fmt.Sprintf("Welcome %s", m.global.User.Username)
	welcomeFig := styles.RenderFigureAscii(welcomeUser, innerWidth)
	welcomeText := styles.Welcome.Render(welcomeFig)

	homePageActions := styles.RenderActionFooter(styles.GlobalActions)

	return lg.Place(
		m.global.Width, m.global.Height,
		lg.Center, lg.Center,
		styles.RenderMainLayout(m.global.Width, m.global.Height, titleText, welcomeText, homePageActions),
	)
}
