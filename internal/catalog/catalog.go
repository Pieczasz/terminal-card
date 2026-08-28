// Package catalog is the single registration point for playable games. Rules and
// TUI view are declared in the same entry.
package catalog

import (
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/tui/router"

	crazyeightrules "github.com/Pieczasz/terminal-card/internal/game/crazyeight"
	ginrummyrules "github.com/Pieczasz/terminal-card/internal/game/ginrummy"
	heartsrules "github.com/Pieczasz/terminal-card/internal/game/hearts"
	pokerrules "github.com/Pieczasz/terminal-card/internal/game/poker"
	unorules "github.com/Pieczasz/terminal-card/internal/game/uno"
	crazyeightview "github.com/Pieczasz/terminal-card/internal/tui/views/game/crazyeight"
	ginrummyview "github.com/Pieczasz/terminal-card/internal/tui/views/game/ginrummy"
	heartsview "github.com/Pieczasz/terminal-card/internal/tui/views/game/hearts"
	pokerview "github.com/Pieczasz/terminal-card/internal/tui/views/game/poker"
	unoview "github.com/Pieczasz/terminal-card/internal/tui/views/game/uno"

	tea "charm.land/bubbletea/v2"
)

type Entry struct {
	Name  string
	Slug  string
	Rules func() game.Rules
	View  func(router.GlobalContext, *game.Engine) tea.Model
}

func (e Entry) Module() game.Module {
	return game.Module{Name: e.Name, Slug: e.Slug, Factory: e.Rules}
}

// All is the single point of game registration: every entry carries both the
// rules factory and the TUI view constructor, and catalog_test fails on a missing
// field or duplicate slug. A new game ships by adding one entry here.
var All = []Entry{
	{
		Name:  "Crazy Eights",
		Slug:  "crazy_eights",
		Rules: func() game.Rules { return &crazyeightrules.Rules{} },
		View:  crazyeightview.New,
	},
	{
		Name:  "Poker",
		Slug:  "poker",
		Rules: func() game.Rules { return &pokerrules.Rules{} },
		View:  pokerview.New,
	},
	{
		Name:  "Uno",
		Slug:  "uno",
		Rules: func() game.Rules { return &unorules.Rules{} },
		View:  unoview.New,
	},
	{
		Name:  "Hearts",
		Slug:  "hearts",
		Rules: func() game.Rules { return &heartsrules.Rules{} },
		View:  heartsview.New,
	},
	{
		Name:  "Gin Rummy",
		Slug:  "gin_rummy",
		Rules: func() game.Rules { return &ginrummyrules.Rules{} },
		View:  ginrummyview.New,
	},
}
