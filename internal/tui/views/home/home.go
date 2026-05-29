package home

import (
	"client/internal/tui/router"
	"client/internal/tui/styles"
	"strings"

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
		case "j":
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "lobby_join"} }
		case "p":
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "profile"} }
		}
	}
	return m, nil
}

func (m model) View() string {
	maxWidth := m.global.Width * 5 / 6
	title := styles.Title.Render("Play card games in your terminal")
	boxStyle := styles.Box.Width(maxWidth).Align(lg.Center)
	mainBox := boxStyle.Render(
		lg.JoinVertical(lg.Center, title),
	)

	rawActions := []string{
		"n - Create new game",
		"j - Join game",
		"p - Your Profile",
		"q - Quit",
	}

	var renderedActions []string
	var totalActionsWidth int

	for i, action := range rawActions {
		style := styles.ActionsText
		if i == len(rawActions)-1 {
			style = style.PaddingRight(0)
		}

		r := style.Render(action)
		renderedActions = append(renderedActions, r)
		totalActionsWidth += lg.Width(r)
	}

	// "space-between"
	numItems := len(renderedActions)
	numGaps := numItems - 1

	var gapSize int
	if numGaps > 0 {
		gapSize = (maxWidth - totalActionsWidth) / numGaps
	}
	if gapSize < 0 {
		gapSize = 0
	}

	spacer := strings.Repeat(" ", gapSize)
	homePageActions := strings.Join(renderedActions, spacer)

	uiStack := lg.JoinVertical(
		lg.Center,
		mainBox,
		lg.NewStyle().MarginTop(1).Render(homePageActions),
	)

	return lg.Place(
		m.global.Width, m.global.Height,
		lg.Center, lg.Center,
		uiStack,
	)
}
