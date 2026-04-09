package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	lg "github.com/charmbracelet/lipgloss"
)

type currentState uint

const (
	homepage currentState = iota
)

type model struct {
	state  currentState
	width  int
	height int
}

func AppModel() tea.Model {
	return model{
		state: homepage,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) View() string {
	// Main box setup
	maxWidth := m.width * 5 / 6
	title := StyleTitle.Render("Play card games in your terminal")
	boxStyle := StyleBox.Width(maxWidth).Align(lg.Center)
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
		style := StyleHomePageActionsText
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
		m.width, m.height,
		lg.Center, lg.Center,
		uiStack,
	)
}
