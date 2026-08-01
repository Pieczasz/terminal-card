package views

import (
	"fmt"

	"github.com/Pieczasz/terminal-card/internal/player"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
)

// SessionPlayer builds the player for the authenticated user, or nil when unauthenticated.
func SessionPlayer(g router.GlobalContext) *player.Player {
	if g.User == nil {
		return nil
	}
	return &player.Player{ID: fmt.Sprint(g.User.ID), DatabaseUser: g.User}
}

// globalActionRoutes maps the shortcut keys advertised in styles.GlobalActions to
// the routes they open. esc and q are deliberately absent: they mean "back", and
// each view decides what back means for it.
var globalActionRoutes = map[string]string{
	"n": router.RouteLobbyCreate,
	"f": router.RouteLobbyJoin,
	"p": router.RouteProfile,
	"t": router.RouteLeaderboard,
}

// GlobalRoute returns the route a global shortcut key opens.
func GlobalRoute(key string) (string, bool) {
	route, ok := globalActionRoutes[key]
	return route, ok
}

func HandleCommonMsg(msg tea.Msg, global *router.GlobalContext) (bool, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		global.Width = msg.Width
		global.Height = msg.Height
		return true, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
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
