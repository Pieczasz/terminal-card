package profile

import (
	"client/internal/db"
	"client/internal/tui/router"
	"client/internal/tui/styles"
	"fmt"

	"client/internal/tui/views/common"

	tea "github.com/charmbracelet/bubbletea"
	lg "github.com/charmbracelet/lipgloss"
)

type model struct {
	global      router.GlobalContext
	userProfile *db.User
	err         error
}

func New(global router.GlobalContext) tea.Model {
	return model{global: global}
}

type profileLoadedMsg struct {
	user *db.User
	err  error
}

func loadProfile(q *db.Queries, userID uint) tea.Cmd {
	return func() tea.Msg {
		user, err := q.GetUserProfile(userID)
		return profileLoadedMsg{user: user, err: err}
	}
}

func (m model) Init() tea.Cmd {
	return loadProfile(m.global.Queries, m.global.User.ID)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if handled, cmd := common.HandleCommonMsg(msg, &m.global); handled {
		return m, cmd
	}

	switch msg := msg.(type) {
	case profileLoadedMsg:
		m.userProfile = msg.user
		m.err = msg.err
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "home"} }
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
	var content string
	if m.err != nil {
		content = fmt.Sprintf("Error loading profile: %v", m.err)
	} else if m.userProfile == nil {
		content = "Loading profile..."
	} else {
		content = fmt.Sprintf("Profile for: %s\nID: %d\n\n(Game History and Settings will go here)",
			m.userProfile.Username, m.userProfile.ID)
	}

	innerWidth := styles.GetInnerWidth(m.global.Width)
	titleFig := styles.RenderFigureAscii("User Profile", innerWidth)
	titleText := styles.Title.Render(titleFig)
	header := styles.Title.Render(titleText)
	footer := lg.NewStyle().Render(styles.RenderActionFooter(styles.GlobalActions))

	return common.RenderCenteredLayout(m.global.Width, m.global.Height, header, content, footer)
}
