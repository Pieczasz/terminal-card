package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	title := StyleTitle.Render("Play card games in your terminal")

	// Box Content
	content := "Press 'q' to disconnect."

	boxWidth := m.width * 5 / 6

	boxStyle := StyleBox.Width(boxWidth)
	box := boxStyle.Render(
		lipgloss.JoinVertical(lipgloss.Center, title, content),
	)

	// Center the box in the terminal
	return lipgloss.Place(
		m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box,
	)
}
