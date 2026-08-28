package tui

import (
	"testing"

	"github.com/Pieczasz/terminal-card/internal/catalog"
	"github.com/Pieczasz/terminal-card/internal/tui/router"

	"github.com/stretchr/testify/assert"
)

// A game in the catalog with no route is a game the lobby can start and then fail to
// navigate to: the player sits in a lobby whose match has begun without them. The
// catalog's own test pins that every entry has a view; this pins that the view is
// actually reachable by the route the lobby derives from the slug.
func TestRegisterGameViews_EveryCatalogSlugResolves(t *testing.T) {
	t.Parallel()

	r := router.New(router.GlobalContext{})
	registerGameViews(r)

	for _, e := range catalog.All {
		assert.Truef(t, r.HasRoute(router.GameRoute(e.Slug)),
			"%s (slug %q) has no registered route", e.Name, e.Slug)
	}
}
