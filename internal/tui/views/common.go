package views

import (
	"slices"

	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/lobby"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
)

func SessionPlayer(g router.GlobalContext) *game.Player {
	return lobby.NewPlayer(g.User)
}

// SessionPlayerID is the seat key the engine and lobby know this user by. It reads the
// ID off the player rather than formatting the user ID again, since a subscription keyed
// on a different spelling silently never fires.
func SessionPlayerID(g router.GlobalContext) string {
	p := SessionPlayer(g)
	if p == nil {
		return ""
	}
	return p.ID
}

// ListenOn delivers the next value from ch as a message. A closed or absent channel
// returns nil rather than blocking, so a view that released its subscription just stops
// updating.
func ListenOn[T any](ch <-chan T, wrap func(T) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return nil
		}
		v, ok := <-ch
		if !ok {
			return nil
		}
		return wrap(v)
	}
}

// esc and q are absent: they mean "back", and each view decides what back means for it.
var globalActionRoutes = map[string]string{
	"n": router.RouteLobbyCreate,
	"f": router.RouteLobbyJoin,
	"p": router.RouteProfile,
	"t": router.RouteLeaderboard,
}

func GlobalRoute(key string) (string, bool) {
	route, ok := globalActionRoutes[key]
	return route, ok
}

// NavigateOn resolves the navigation keys every full-screen view shares: the global
// shortcuts, plus esc/q for "back to home". Views where esc means something else (the
// lobby's leave confirmation) call GlobalRoute directly instead.
func NavigateOn(key string) (tea.Cmd, bool) {
	if route, ok := GlobalRoute(key); ok {
		return func() tea.Msg { return router.ChangeViewMsg{ViewName: route} }, true
	}
	if key == "esc" || key == "q" {
		return func() tea.Msg { return router.ChangeViewMsg{ViewName: router.RouteHome} }, true
	}
	return nil, false
}

func HandleCommonMsg(msg tea.Msg, global *router.GlobalContext) (bool, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		global.Width = msg.Width
		global.Height = msg.Height
		return true, nil
	case tea.BackgroundColorMsg:
		// The router updates its own copy for views built later; this one is on screen
		// now, so a mid-session theme switch lands without navigating away.
		global.Theme = styles.NewTheme(msg.IsDark())
		return true, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			return true, tea.Quit
		}
	}
	return false, nil
}

// RenderScreen is the header/footer/frame ritual every full-screen view repeats.
// content receives the rows left once the title and footer have taken theirs.
//
// slices.Concat, never append: styles.GlobalActions is shared by every session, and
// appending writes into the array the others are reading.
func RenderScreen(g router.GlobalContext, title string, localActions []string, content func(height int) string) string {
	header := g.Theme.Title.Render(styles.RenderFigureASCII(title, styles.InnerWidth(g.Width)))
	footer := g.Theme.RenderActionFooter(slices.Concat(localActions, styles.GlobalActions))
	return RenderCenteredLayout(g, header, content(styles.AvailableContentHeight(g.Height, header, footer)), footer)
}

// RenderCenteredLayout frames content for a full-screen view.
func RenderCenteredLayout(g router.GlobalContext, header, content, footer string) string {
	return styles.Place(
		g.Width, g.Height,
		lg.Center, lg.Center,
		g.Theme.RenderMainLayout(g.Width, g.Height, header, content, footer),
	)
}
