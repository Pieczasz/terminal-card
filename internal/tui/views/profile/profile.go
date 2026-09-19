package profile

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
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
	historyFetchLimit = 50
	filterAllGames    = "All"
	filterAllResults  = "All"
	filterWins        = "Wins"
	filterLosses      = "Losses"

	// Fixed cells so cycling filters or empty history cannot resize the layout.
	colGame   = 12 // longest catalog name today: "Crazy Eights"
	colElo    = 4
	colPlace  = 10 // "1st place"
	colResult = 14 // "Elo change: +99" / "casual game"
	// tableGap is the space between the two tables when they sit side by side, and
	// what the fit check has to account for when deciding whether they can.
	tableGap = 4
	// twoTableMinHeight is what a stacked pair costs at its smallest: the two label
	// lines, two spacers, two 2-line table headers, a row each, and the gap between
	// them. Below it one table has to go.
	twoTableMinHeight = 11
)

type model struct {
	global      router.GlobalContext
	userProfile *db.User
	history     []db.MatchParticipant
	err         error
	historyErr  error

	gameFilters   []string
	gameFilterIdx int
	resultFilters []string
	resultIdx     int
}

func New(global router.GlobalContext) tea.Model {
	gameFilters := make([]string, 0, 1+len(catalog.All))
	gameFilters = append(gameFilters, filterAllGames)
	for _, e := range catalog.All {
		gameFilters = append(gameFilters, e.Name)
	}
	return model{
		global:        global,
		gameFilters:   gameFilters,
		resultFilters: []string{filterAllResults, filterWins, filterLosses},
	}
}

// profileLoadedMsg keeps the two failures apart. They are two queries, and a
// player whose match history could not be read still has a profile worth showing.
type profileLoadedMsg struct {
	user       *db.User
	history    []db.MatchParticipant
	err        error
	historyErr error
}

func loadProfile(ctx context.Context, userRepo db.UserRepository, userID uint) tea.Cmd {
	return func() tea.Msg {
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		user, err := userRepo.UserProfile(reqCtx, userID)
		if err != nil {
			return profileLoadedMsg{err: err}
		}
		history, historyErr := userRepo.UserMatchHistory(reqCtx, userID, historyFetchLimit)
		return profileLoadedMsg{user: user, history: history, historyErr: historyErr}
	}
}

func (m model) Init() tea.Cmd {
	if m.global.User == nil {
		return nil
	}
	return loadProfile(m.global.RequestContext(), m.global.UserRepository, m.global.User.ID)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if handled, cmd := views.HandleCommonMsg(msg, &m.global); handled {
		return m, cmd
	}
	switch msg := msg.(type) {
	case profileLoadedMsg:
		m.userProfile = msg.user
		m.history = msg.history
		m.err = msg.err
		m.historyErr = msg.historyErr
		if msg.err != nil {
			slog.Error("database error while loading user profile", "error", msg.err)
		}
		if msg.historyErr != nil {
			slog.Error("database error while loading match history", "error", msg.historyErr)
		}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "g":
			m.gameFilterIdx = components.CycleIndex(m.gameFilterIdx, 1, len(m.gameFilters))
			return m, nil
		case "r":
			m.resultIdx = components.CycleIndex(m.resultIdx, 1, len(m.resultFilters))
			return m, nil
		}
		if cmd, ok := views.NavigateOn(msg.String()); ok {
			return m, cmd
		}
	}
	return m, nil
}

func (m model) View() tea.View {
	actions := []string{"g - Game", "r - Result"}
	return tea.NewView(views.RenderScreen(m.global, "User Profile", actions, m.renderContent))
}

func (m model) renderContent(contentHeight int) string {
	if m.err != nil {
		return "Unable to load profile. Please try again."
	}
	if m.userProfile == nil {
		return "Loading profile..."
	}

	// userInfo, spacer, filter, spacer, and the table header - which is two lines,
	// its titles and the rule under them.
	const extraVerticalLines = 6
	maxItems := max(contentHeight-extraVerticalLines, 1)

	// The two tables are fixed-width, so below a certain terminal they do not fit
	// beside each other and lipgloss word-wraps the columns into confetti rather
	// than shrinking them. Stacking is what renderForm does for the same reason.
	stacked := rankingsTable.Width()+tableGap+historyTable.Width() > styles.InnerWidth(m.global.Width)
	rankItems, histItems := maxItems, maxItems
	if stacked {
		// Both tables now spend height instead of sharing it: two headers and the
		// spacer between them come out of the same budget.
		rankItems = max((maxItems-3)/2, 1)
		histItems = max(maxItems-3-rankItems, 1)
	}

	userInfo := fmt.Sprintf("Profile for: %s", m.userProfile.Username)
	filters := m.global.Theme.Muted.Render(fmt.Sprintf("Game: %s  Result: %s",
		styles.PadTruncate(m.gameFilters[m.gameFilterIdx], colGame),
		styles.PadTruncate(m.resultFilters[m.resultIdx], len(filterLosses)),
	))

	// At the declared 64x20 minimum the title and footer leave six lines, fewer than
	// two stacked tables need at their smallest. The rankings summary gives way to
	// the match history, which is what a player opens this screen for.
	if stacked && contentHeight < twoTableMinHeight {
		items := max(contentHeight-4, 1) // the two labels and the 2-line header
		return lg.JoinVertical(lg.Left, userInfo, filters,
			lg.JoinVertical(lg.Left, m.historyRows(items)...))
	}

	rankingsStyle := lg.NewStyle().Align(lg.Left).Width(rankingsTable.Width())
	if !stacked {
		rankingsStyle = rankingsStyle.MarginRight(tableGap)
	}
	rankingsCol := rankingsStyle.Render(lg.JoinVertical(lg.Left, m.rankingRows(rankItems)...))
	historyCol := lg.NewStyle().Align(lg.Left).Width(historyTable.Width()).
		Render(lg.JoinVertical(lg.Left, m.historyRows(histItems)...))

	tables := lg.JoinHorizontal(lg.Top, rankingsCol, historyCol)
	if stacked {
		tables = lg.JoinVertical(lg.Left, rankingsCol, "", historyCol)
	}

	return lg.JoinVertical(lg.Left, userInfo, "", filters, "", tables)
}

// The two tables' cells are fixed so cycling a filter cannot resize the layout.
var (
	rankingsTable = components.Table{Cols: []components.Column{
		{Title: "Game", Width: colGame},
		{Title: "Elo", Width: colElo},
	}}
	historyTable = components.Table{Cols: []components.Column{
		{Title: "Game", Width: colGame},
		{Title: "Place", Width: colPlace},
		{Title: "Result", Width: colResult},
	}}
)

// limitRows splits n items into what fits and whether to say so. The "... and more"
// line comes *out* of the budget rather than being appended past it, or a truncated
// table is one line taller than the space it was given.
func limitRows(n, maxItems int) (show int, more bool) {
	if n <= maxItems {
		return n, false
	}
	return max(maxItems-1, 0), true
}

func (m model) rankingRows(maxItems int) []string {
	rows := []string{rankingsTable.Header(m.global.Theme)}
	if len(m.userProfile.Rankings) == 0 {
		return append(rows, styles.PadTruncate("No games yet.", rankingsTable.Width()))
	}
	show, more := limitRows(len(m.userProfile.Rankings), maxItems)
	for _, r := range m.userProfile.Rankings[:show] {
		rows = append(rows, rankingsTable.Cells(r.Game.Name, strconv.FormatUint(uint64(r.Elo), 10)))
	}
	if more {
		rows = append(rows, "... and more")
	}
	return rows
}

func (m model) filteredHistory() []db.MatchParticipant {
	out := make([]db.MatchParticipant, 0, len(m.history))
	wantGame := m.gameFilters[m.gameFilterIdx]
	wantResult := m.resultFilters[m.resultIdx]
	for _, h := range m.history {
		if wantGame != filterAllGames && h.Match.Game.Name != wantGame {
			continue
		}
		won := h.Placement == 1
		switch wantResult {
		case filterWins:
			if !won {
				continue
			}
		case filterLosses:
			if won {
				continue
			}
		}
		out = append(out, h)
	}
	return out
}

func (m model) historyRows(maxItems int) []string {
	rows := []string{historyTable.Header(m.global.Theme)}
	if m.historyErr != nil {
		return append(rows, m.global.Theme.ErrorText.Render("Unable to load match history."))
	}
	filtered := m.filteredHistory()
	if len(filtered) == 0 {
		return append(rows, styles.PadTruncate("No matches for this filter.", historyTable.Width()))
	}
	show, more := limitRows(len(filtered), maxItems)
	for _, h := range filtered[:show] {
		rows = append(rows, historyTable.Cells(
			h.Match.Game.Name, placementPlain(h.Placement), resultPlain(h)))
	}
	if more {
		rows = append(rows, "... and more")
	}
	return rows
}

func resultPlain(h db.MatchParticipant) string {
	if !h.Match.Ranked {
		return "casual game"
	}
	if h.EloDelta >= 0 {
		return fmt.Sprintf("Elo +%d", h.EloDelta)
	}
	return fmt.Sprintf("Elo %d", h.EloDelta)
}

func placementPlain(placement int) string {
	switch placement {
	case 1, 2, 3:
		return placementWords[placement-1]
	default:
		return fmt.Sprintf("%d place", placement)
	}
}

var placementWords = [3]string{"1st place", "2nd place", "3rd place"}
