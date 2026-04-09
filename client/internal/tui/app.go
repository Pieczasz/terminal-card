package tui

import tea "github.com/charmbracelet/bubbletea"

type currentState uint

const (
	homepage currentState = iota
)

type model struct {
	state currentState
}

func NewModel() model {
	// 	s := spinner.New()

	return model{
		state: homepage,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}
