package leaderboard

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Pieczasz/terminal-card/internal/catalog"
	"github.com/Pieczasz/terminal-card/internal/tui/components"
	"github.com/Pieczasz/terminal-card/internal/tui/views"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"
)

const (
	// pageSize is how many ranks fit on one screen of the table. The board does not
	// grow with the terminal: extra height is empty, not more rows.
	pageSize = 20
	// maxLeaderboardPlayers is the hardest cap on how far pagination can go.
	maxLeaderboardPlayers = 200

	minPlayerWidth = 10
	maxPlayerWidth = 16

	colRank = 5
	colGame = 15
	colElo  = 5
)

// filterAll is the empty BestPlayers gameName: every ranking across every game.
const filterAll = "All"

type model struct {
	global      router.GlobalContext
	rankings    []db.Ranking
	err         error
	filters     []string
	filterIndex int
	page        int
	loading     bool
	// exhausted means the last fetch returned everything the repository has (or
	// we already hold maxLeaderboardPlayers), so paging further must not re-query.
	exhausted bool
}

func New(global router.GlobalContext) tea.Model {
	filters := make([]string, 0, 1+len(catalog.All))
	filters = append(filters, filterAll)
	for _, e := range catalog.All {
		filters = append(filters, e.Name)
	}
	return model{global: global, filters: filters}
}

type loadedMsg struct {
	rankings []db.Ranking
	err      error
	wantPage int
}

func (m model) gameFilter() string {
	if m.filterIndex == 0 {
		return ""
	}
	return m.filters[m.filterIndex]
}

func (m model) pageCount() int {
	if len(m.rankings) == 0 {
		return 1
	}
	return (len(m.rankings) + pageSize - 1) / pageSize
}

func (m model) needsFetch(page int) int {
	// A short last page still covers that page index; only ask for more when the
	// cursor would land past what we already hold.
	if page*pageSize < len(m.rankings) || m.exhausted {
		return 0
	}
	need := min((page+1)*pageSize, maxLeaderboardPlayers)
	if need <= len(m.rankings) {
		return 0
	}
	return need
}

func (m model) load(limit int, wantPage int) tea.Cmd {
	gameName := m.gameFilter()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.global.RequestContext(), 5*time.Second)
		defer cancel()
		rankings, err := m.global.UserRepository.BestPlayers(ctx, limit, gameName)
		return loadedMsg{rankings: rankings, err: err, wantPage: wantPage}
	}
}

func (m model) Init() tea.Cmd {
	return m.load(pageSize, 0)
}

func (m model) cycleFilter(delta int) (tea.Model, tea.Cmd) {
	m.filterIndex = components.CycleIndex(m.filterIndex, delta, len(m.filters))
	m.rankings = nil
	m.err = nil
	m.page = 0
	m.exhausted = false
	m.loading = true
	return m, m.load(pageSize, 0)
}

func (m model) goPage(delta int) (tea.Model, tea.Cmd) {
	next := m.page + delta
	if next < 0 {
		return m, nil
	}
	if need := m.needsFetch(next); need > 0 {
		m.loading = true
		return m, m.load(need, next)
	}
	if next >= m.pageCount() {
		return m, nil
	}
	m.page = next
	return m, nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if handled, cmd := views.HandleCommonMsg(msg, &m.global); handled {
		return m, cmd
	}
	switch msg := msg.(type) {
	case loadedMsg:
		m.loading = false
		m.rankings = msg.rankings
		m.err = msg.err
		if msg.err != nil {
			slog.Error("database error while fetching leaderboard", "error", msg.err)
			return m, nil
		}
		// Fewer rows than the request (or the hard cap) means there is nothing left to page into.
		asked := min((msg.wantPage+1)*pageSize, maxLeaderboardPlayers)
		m.exhausted = len(msg.rankings) < asked || len(msg.rankings) >= maxLeaderboardPlayers
		m.page = min(msg.wantPage, max(m.pageCount()-1, 0))
	case tea.KeyPressMsg:
		switch msg.String() {
		case "g":
			return m.cycleFilter(1)
		case "right", "l":
			return m.cycleFilter(1)
		case "left", "h":
			return m.cycleFilter(-1)
		case "down", "j", "pgdown", "n":
			return m.goPage(1)
		case "up", "k", "pgup", "p":
			return m.goPage(-1)
		}
		if cmd, ok := views.NavigateOn(msg.String()); ok {
			return m, cmd
		}
	}
	return m, nil
}

func (m model) View() tea.View {
	actions := []string{"g/←/→ - Filter: " + m.filters[m.filterIndex], "↑/↓ - Page"}

	return tea.NewView(views.RenderScreen(m.global, "Leaderboard", actions, func(int) string {
		switch {
		case m.err != nil:
			return m.renderError()
		case m.rankings == nil || m.loading && len(m.rankings) == 0:
			return m.renderLoading()
		case len(m.rankings) == 0:
			return m.renderEmpty()
		default:
			return m.renderRankings(styles.InnerWidth(m.global.Width))
		}
	}))
}

func (m model) renderError() string {
	return lg.JoinVertical(lg.Center, "Unable to load leaderboard. Please try again.")
}

func (m model) renderLoading() string {
	return lg.JoinVertical(lg.Center, "Loading leaderboard.")
}

func (m model) renderEmpty() string {
	if m.filterIndex == 0 {
		return lg.JoinVertical(lg.Center, "No players have ranked yet.")
	}
	return lg.JoinVertical(lg.Center, fmt.Sprintf("No rankings for %s yet.", m.filters[m.filterIndex]))
}

// table is the fixed-cell board. The player column is the only one that flexes, and
// only with the terminal: the rest stay put so paging cannot shift the columns.
func (m model) table(contentWidth int) components.Table {
	playerWidth := min(max(contentWidth-(colRank+colGame+colElo+9), minPlayerWidth), maxPlayerWidth)
	return components.Table{
		Cols: []components.Column{
			{Title: "Rank", Width: colRank},
			{Title: "Player", Width: playerWidth},
			{Title: "Game", Width: colGame},
			{Title: "Elo", Width: colElo},
		},
		PadTo: pageSize,
	}
}

func (m model) renderRankings(contentWidth int) string {
	tbl := m.table(contentWidth)
	start := m.page * pageSize
	end := min(start+pageSize, len(m.rankings))

	rows := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		rows = append(rows, m.renderPlayerRow(tbl, i, m.rankings[i]))
	}

	table := tbl.Render(m.global.Theme, rows)
	heading := m.global.Theme.Title.
		Bold(true).
		MarginBottom(1).
		Render("Filter: " + m.filters[m.filterIndex])
	pager := m.global.Theme.Dim.Render(fmt.Sprintf("page %d/%d  ranks %d-%d of %d",
		m.page+1, m.pageCount(), start+1, end, len(m.rankings)))
	if m.loading {
		pager = m.global.Theme.Dim.Render("loading…")
	}
	return lg.JoinVertical(lg.Center, "", heading, table, pager)
}

// renderPlayerRow lays the cells out itself rather than through Table.Cells: the
// viewer's own row is highlighted, and a styled cell cannot be padded by rune count
// afterwards without counting the escape sequence as text.
func (m model) renderPlayerRow(tbl components.Table, index int, r db.Ranking) string {
	playerWidth := tbl.Cols[1].Width
	userStr := styles.PadTruncate(r.User.Username, playerWidth)
	if m.global.User != nil && r.User.ID == m.global.User.ID {
		userStr = m.global.Theme.PlayerItemSelected.Bold(true).Render(userStr)
	}
	return fmt.Sprintf("%-*d | %s | %-*s | %d",
		colRank, index+1, userStr, colGame, styles.PadTruncate(r.Game.Name, colGame), r.Elo)
}
