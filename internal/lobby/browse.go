package lobby

import (
	"slices"
	"strings"

	"github.com/Pieczasz/terminal-card/internal/elo"
	"github.com/Pieczasz/terminal-card/internal/player"
)

// DefaultBrowseLimit is how many tables the browser offers. A player scrolling a
// hundred lobbies is not choosing between them, and every extra row is another
// lobby lock taken on every refresh, for every viewer.
const DefaultBrowseLimit = 20

// BrowseMode filters the list by whether a table moves Elo.
type BrowseMode uint8

const (
	BrowseAny BrowseMode = iota
	BrowseRanked
	BrowseCasual
)

// BrowseEntry is one row of the lobby browser, snapshotted under the lobby's own
// lock. Rendering reads the struct and never touches the lobby again: the list
// refreshes on a timer, so going back through the getters would take five locks per
// row per frame.
type BrowseEntry struct {
	Code       string
	GameName   string
	Players    int
	MaxPlayers int
	Ranked     bool
	AvgElo     uint32
	// EloDistance is how far this table's average sits from the browsing player's
	// rating in the same game. It is what the list is ordered by.
	EloDistance int
}

// HasRoom reports whether another player still fits.
func (e BrowseEntry) HasRoom() bool { return e.Players < e.MaxPlayers }

type BrowseFilter struct {
	// GameName empty means any game.
	GameName string
	Mode     BrowseMode
	// OnlyWithRoom hides tables that are already full.
	OnlyWithRoom bool
	// Limit caps the rows returned; zero means DefaultBrowseLimit.
	Limit int
}

func (f BrowseFilter) matches(e BrowseEntry) bool {
	if f.GameName != "" && e.GameName != f.GameName {
		return false
	}
	if f.OnlyWithRoom && !e.HasRoom() {
		return false
	}
	switch f.Mode {
	case BrowseRanked:
		return e.Ranked
	case BrowseCasual:
		return !e.Ranked
	case BrowseAny:
	}
	return true
}

// BrowseLobbies returns the tables most worth offering p, closest rating first and
// capped by the filter's limit. The underlying public-lobby set is cached, so many
// players browsing at once still costs one scan per cache window.
func (m *Manager) BrowseLobbies(p *player.Player, f BrowseFilter) []BrowseEntry {
	if m == nil {
		return nil
	}
	lobbies := m.getCachedPublicLobbies()
	ratings := playerRatings(p)

	entries := make([]BrowseEntry, 0, min(len(lobbies), f.limit()))
	for _, l := range lobbies {
		entry := l.browseEntry()
		if !f.matches(entry) {
			continue
		}
		entry.EloDistance = abs(int(entry.AvgElo) - int(ratingFor(ratings, entry.GameName)))
		entries = append(entries, entry)
	}

	// Code breaks ties so the list does not reshuffle under the cursor between two
	// refreshes that are a second apart.
	slices.SortFunc(entries, func(a, b BrowseEntry) int {
		if a.EloDistance != b.EloDistance {
			return a.EloDistance - b.EloDistance
		}
		return strings.Compare(a.Code, b.Code)
	})

	if len(entries) > f.limit() {
		entries = entries[:f.limit()]
	}
	return entries
}

func (f BrowseFilter) limit() int {
	if f.Limit <= 0 {
		return DefaultBrowseLimit
	}
	return f.Limit
}

// playerRatings indexes a player's per-game Elo. An unnamed game is skipped: it
// matches no lobby and would otherwise become the target rating for all of them.
func playerRatings(p *player.Player) map[string]uint32 {
	if p == nil || p.DatabaseUser == nil {
		return nil
	}
	ratings := make(map[string]uint32, len(p.DatabaseUser.Rankings))
	for _, r := range p.DatabaseUser.Rankings {
		if r.Game.Name != "" {
			ratings[r.Game.Name] = r.Elo
		}
	}
	return ratings
}

// ratingFor is the player's rating in gameName, or the starting rating when they
// have never played it - a newcomer is matched against tables near the default.
func ratingFor(ratings map[string]uint32, gameName string) uint32 {
	if rating := ratings[gameName]; rating != 0 {
		return rating
	}
	return elo.ToUint32(elo.DefaultRating)
}

// browseEntry snapshots the row under one lock.
func (l *Lobby) browseEntry() BrowseEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	gameName := ""
	if l.options.cardGame != nil {
		gameName = l.options.cardGame.Name
	}
	return BrowseEntry{
		Code:       l.code,
		GameName:   gameName,
		Players:    1 + len(l.guests),
		MaxPlayers: l.options.maxPlayers,
		Ranked:     l.options.isRanked,
		AvgElo:     l.averageEloLocked(gameName),
	}
}

// GameNames lists the games that currently have a public table, for the browser's
// game filter. Sorted so the filter cycles in a stable order.
func (m *Manager) GameNames() []string {
	if m == nil {
		return nil
	}
	seen := map[string]struct{}{}
	for _, l := range m.getCachedPublicLobbies() {
		if name := l.GameName(); name != "" {
			seen[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
