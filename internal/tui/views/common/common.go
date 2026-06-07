package common

import (
	"client/internal/tui/router"
	"client/internal/tui/styles"

	tea "github.com/charmbracelet/bubbletea"
	lg "github.com/charmbracelet/lipgloss"
)

func HandleCommonMsg(msg tea.Msg, global *router.GlobalContext) (bool, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		global.Width = msg.Width
		global.Height = msg.Height
		return true, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return true, tea.Quit
		}
	}
	return false, nil
}

func RenderCenteredLayout(width, height int, header, content, footer string) string {
	return lg.Place(
		width, height,
		lg.Center, lg.Center,
		styles.RenderMainLayout(width, height, header, content, footer),
	)
}
