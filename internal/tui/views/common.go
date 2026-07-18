package views

import (
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
)

func HandleCommonMsg(msg tea.Msg, global *router.GlobalContext) (bool, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		global.Width = msg.Width
		global.Height = msg.Height
		return true, nil
	case tea.KeyPressMsg:
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
